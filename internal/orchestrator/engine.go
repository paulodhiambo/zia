package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"zia/internal/connector"
	"zia/internal/domain"
	"zia/internal/idempotency"
	"zia/internal/ledger"
	"zia/internal/notification"
	"zia/internal/repository"
	"zia/internal/risk"
	"zia/internal/routing"
	"zia/internal/telemetry"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

const queryDelay = 40 * time.Second

type FeeConfig struct {
	Percent       int   // percentage (e.g. 5 = 5%)
	MinAmount     int64 // minimum fee in minor units (e.g. 100 = 1.00)
}

type Engine struct {
	piRepo      repository.PaymentIntentRepository
	attRepo     repository.AttemptRepository
	registry    *connector.Registry
	router      *routing.Engine
	risk        *risk.Engine
	idempotency *idempotency.Store
	notif       *notification.Dispatcher
	logger      *zap.Logger
	ledger      *ledger.Engine
	feeCfg      FeeConfig
}

func New(
	piRepo repository.PaymentIntentRepository,
	attRepo repository.AttemptRepository,
	registry *connector.Registry,
	router *routing.Engine,
	risk *risk.Engine,
	idempotency *idempotency.Store,
	logger *zap.Logger,
	ledger *ledger.Engine,
	notif *notification.Dispatcher,
	feeCfg FeeConfig,
) *Engine {
	return &Engine{
		piRepo:      piRepo,
		attRepo:     attRepo,
		registry:    registry,
		router:      router,
		risk:        risk,
		idempotency: idempotency,
		notif:       notif,
		logger:      logger,
		ledger:      ledger,
		feeCfg:      feeCfg,
	}
}

type CreatePIRequest struct {
	MerchantID     string              `json:"merchant_id"`
	AmountMinor    int64               `json:"amount_minor"`
	Currency       string              `json:"currency"`
	Method         domain.PaymentMethod `json:"method"`
	CustomerRef    string              `json:"customer_ref,omitempty"`
	CustomerPhone  string              `json:"customer_phone,omitempty"`
	CustomerEmail  string              `json:"customer_email,omitempty"`
	IdempotencyKey string              `json:"idempotency_key,omitempty"`
	Metadata       []byte              `json:"metadata,omitempty"`
}

type CreatePIResult struct {
	PaymentIntent *domain.PaymentIntent
	NextAction    *connector.NextAction
}

func (e *Engine) CreatePaymentIntent(ctx context.Context, req CreatePIRequest) (*CreatePIResult, error) {
	if req.IdempotencyKey != "" {
		existing, err := e.idempotency.Check(ctx, req.MerchantID, req.IdempotencyKey)
		if err != nil {
			return nil, fmt.Errorf("idempotency check: %w", err)
		}
		if existing != "" {
			pi, err := e.piRepo.GetByID(ctx, existing)
			if err != nil {
				return nil, err
			}
			e.logger.Info("idempotency hit, returning existing pi",
				zap.String("pi_id", pi.ID),
				zap.String("merchant_id", req.MerchantID),
				zap.String("idempotency_key", req.IdempotencyKey),
			)
			return &CreatePIResult{PaymentIntent: pi}, nil
		}
	}

	e.logger.Info("evaluating risk",
		zap.String("merchant_id", req.MerchantID),
		zap.Int64("amount_minor", req.AmountMinor),
		zap.String("currency", req.Currency),
		zap.String("method", string(req.Method)),
	)

	if err := e.risk.Evaluate(ctx, risk.Request{
		MerchantID:    req.MerchantID,
		AmountMinor:   req.AmountMinor,
		Currency:      req.Currency,
		Method:        string(req.Method),
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: req.CustomerEmail,
	}); err != nil {
		e.logger.Warn("risk check rejected",
			zap.String("merchant_id", req.MerchantID),
			zap.Int64("amount_minor", req.AmountMinor),
			zap.Error(err),
		)
		return nil, fmt.Errorf("risk check: %w", err)
	}

	e.logger.Info("risk check passed, routing",
		zap.String("merchant_id", req.MerchantID),
		zap.String("currency", req.Currency),
		zap.String("method", string(req.Method)),
	)

	route, err := e.router.Route(ctx, routing.RouteRequest{
		MerchantID:  req.MerchantID,
		Currency:    req.Currency,
		Method:      string(req.Method),
		AmountMinor: req.AmountMinor,
	})
	if err != nil {
		e.logger.Error("routing failed",
			zap.String("merchant_id", req.MerchantID),
			zap.String("method", string(req.Method)),
			zap.Error(err),
		)
		return nil, fmt.Errorf("routing: %w", err)
	}

	e.logger.Info("route selected",
		zap.String("merchant_id", req.MerchantID),
		zap.String("primary_psp", route.Primary),
		zap.Strings("fallback_psps", route.Fallbacks),
	)

	conn, ok := e.registry.Get(route.Primary)
	if !ok {
		e.logger.Warn("primary connector not registered, trying fallbacks",
			zap.String("primary", route.Primary),
			zap.String("merchant_id", req.MerchantID),
		)
		for _, fb := range route.Fallbacks {
			if c, found := e.registry.Get(fb); found {
				conn = c
				route.Primary = fb
				ok = true
				e.logger.Info("switched to fallback connector", zap.String("fallback", fb))
				break
			}
		}
		if !ok {
			e.logger.Error("no registered connector for method",
				zap.String("method", string(req.Method)),
				zap.String("merchant_id", req.MerchantID),
			)
			return nil, fmt.Errorf("no registered connector for method=%s", string(req.Method))
		}
	}

	now := time.Now().UTC()

	// Generate a unique idempotency key when the caller doesn't provide one
	// so the partial unique index idx_pi_idempotency (merchant_id, idempotency_key
	// WHERE idempotency_key IS NOT NULL) doesn't reject duplicate empty strings.
	ik := req.IdempotencyKey
	if ik == "" {
		ik = uuid.New().String()
	}

	pi := &domain.PaymentIntent{
		ID:             uuid.New().String(),
		MerchantID:     req.MerchantID,
		AmountMinor:    req.AmountMinor,
		Currency:       req.Currency,
		Status:         domain.PICreated,
		Method:         req.Method,
		CustomerRef:    req.CustomerRef,
		CustomerPhone:  req.CustomerPhone,
		CustomerEmail:  req.CustomerEmail,
		IdempotencyKey: ik,
		Metadata:       req.Metadata,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	expiresAt := now.Add(30 * time.Minute)
	pi.ExpiresAt = &expiresAt

	if err := e.piRepo.Create(ctx, pi); err != nil {
		e.logger.Error("save payment intent failed",
			zap.String("pi_id", pi.ID),
			zap.Error(err),
		)
		return nil, fmt.Errorf("save payment intent: %w", err)
	}

	e.logger.Info("payment intent saved",
		zap.String("pi_id", pi.ID),
		zap.String("merchant_id", pi.MerchantID),
		zap.Int64("amount_minor", pi.AmountMinor),
		zap.String("currency", pi.Currency),
		zap.String("method", string(pi.Method)),
		zap.String("status", string(pi.Status)),
	)

	if req.IdempotencyKey != "" {
		if err := e.idempotency.Store(ctx, req.MerchantID, req.IdempotencyKey, pi.ID); err != nil {
			e.logger.Warn("failed to store idempotency key", zap.Error(err))
		}
	}

	colReq := connector.CollectionRequest{
		PaymentIntentID: pi.ID,
		AmountMinor:     req.AmountMinor,
		Currency:        req.Currency,
		Method:          string(req.Method),
		CustomerPhone:   req.CustomerPhone,
		CustomerEmail:   req.CustomerEmail,
		CallbackURL:     "", // TODO: derive from config
		IdempotencyKey:  pi.ID,
	}

	e.logger.Info("calling connector InitiateCollection",
		zap.String("psp", route.Primary),
		zap.String("pi_id", pi.ID),
		zap.Int64("amount_minor", colReq.AmountMinor),
		zap.String("currency", colReq.Currency),
		zap.String("customer_phone", colReq.CustomerPhone),
	)

	attrs := metric.WithAttributes(
		attribute.String("psp", route.Primary),
		attribute.String("method", string(req.Method)),
	)
	telemetry.PaymentAttempts.Add(ctx, 1, attrs)

	colStart := time.Now()
	colResult, err := conn.InitiateCollection(ctx, colReq)
	telemetry.ConnectorLatency.Record(ctx, time.Since(colStart).Seconds(), attrs)

	if err != nil {
		e.logger.Error("connector InitiateCollection failed",
			zap.String("psp", route.Primary),
			zap.String("pi_id", pi.ID),
			zap.Error(err),
		)
		pi.Status = domain.PIFailed
		pi.UpdatedAt = time.Now().UTC()
		if err := e.piRepo.UpdateStatus(ctx, pi.ID, domain.PIFailed); err != nil {
			return nil, fmt.Errorf("update pi status: %w", err)
		}
		e.logger.Info("payment intent marked failed after connector error",
			zap.String("pi_id", pi.ID),
			zap.String("psp", route.Primary),
		)
		return &CreatePIResult{PaymentIntent: pi}, nil
	}

	e.logger.Info("connector InitiateCollection succeeded",
		zap.String("psp", route.Primary),
		zap.String("pi_id", pi.ID),
		zap.String("psp_reference", colResult.PSPReference),
		zap.String("col_status", colResult.Status),
	)

	attemptID := uuid.New().String()
	attemptStatus := domain.AttemptRequiresAction
	if colResult.Status == "processing" {
		attemptStatus = domain.AttemptProcessing
	}

	reqPayload, _ := json.Marshal(req)

	attempt := &domain.Attempt{
		ID:              attemptID,
		PaymentIntentID: pi.ID,
		PSP:             route.Primary,
		PSPReference:    colResult.PSPReference,
		Status:          attemptStatus,
		SequenceNo:      1,
		RawRequest:      colResult.RawRequest,
		RawResponse:     colResult.RawResponse,
		RequestPayload:  reqPayload,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := e.attRepo.Create(ctx, attempt); err != nil {
		return nil, fmt.Errorf("save attempt: %w", err)
	}

	e.logger.Info("attempt saved",
		zap.String("attempt_id", attempt.ID),
		zap.String("pi_id", pi.ID),
		zap.String("psp", attempt.PSP),
		zap.String("psp_reference", attempt.PSPReference),
		zap.String("status", string(attempt.Status)),
	)

	newStatus := domain.PIRequiresAction
	if colResult.Status == "processing" {
		newStatus = domain.PIProcessing
	}
	pi.Status = newStatus
	pi.UpdatedAt = time.Now().UTC()

	if !IsValidTransition(domain.PICreated, newStatus) {
		return nil, fmt.Errorf("invalid transition from %s to %s", domain.PICreated, newStatus)
	}
	if err := e.piRepo.UpdateStatus(ctx, pi.ID, newStatus); err != nil {
		return nil, fmt.Errorf("update pi status: %w", err)
	}

	e.logger.Info("payment intent status updated",
		zap.String("pi_id", pi.ID),
		zap.String("from", string(domain.PICreated)),
		zap.String("to", string(newStatus)),
	)

	if route.Primary == "mpesa" && newStatus == domain.PIRequiresAction {
		e.scheduleDelayedQuery(pi.ID, attempt.ID, route.Primary)
	}

	return &CreatePIResult{
		PaymentIntent: pi,
		NextAction:    colResult.NextAction,
	}, nil
}

func (e *Engine) GetPaymentIntent(ctx context.Context, id string) (*domain.PaymentIntent, error) {
	return e.piRepo.GetByID(ctx, id)
}

func (e *Engine) HandleWebhookEvent(ctx context.Context, evt connector.WebhookEvent) error {
	e.logger.Info("handling webhook event",
		zap.String("psp", evt.PSP),
		zap.String("psp_reference", evt.PSPReference),
		zap.String("event_type", evt.EventType),
		zap.String("status", evt.Status),
	)

	attempt, err := e.attRepo.GetByPSPReference(ctx, evt.PSP, evt.PSPReference)
	if err != nil {
		e.logger.Error("lookup attempt by psp ref",
			zap.String("psp", evt.PSP),
			zap.String("psp_reference", evt.PSPReference),
			zap.Error(err),
		)
		return fmt.Errorf("lookup attempt by psp ref: %w", err)
	}

	pi, err := e.piRepo.GetByID(ctx, attempt.PaymentIntentID)
	if err != nil {
		e.logger.Error("lookup pi from attempt",
			zap.String("attempt_id", attempt.ID),
			zap.String("pi_id", attempt.PaymentIntentID),
			zap.Error(err),
		)
		return fmt.Errorf("lookup pi: %w", err)
	}

	e.logger.Info("webhook matched attempt",
		zap.String("pi_id", pi.ID),
		zap.String("attempt_id", attempt.ID),
		zap.String("current_pi_status", string(pi.Status)),
		zap.String("current_attempt_status", string(attempt.Status)),
		zap.String("webhook_status", evt.Status),
	)

	if evt.PSPTransactionID != "" {
		if err := e.attRepo.UpdatePSPTransactionID(ctx, attempt.ID, evt.PSPTransactionID); err != nil {
			e.logger.Error("failed to store psp transaction id",
				zap.String("attempt_id", attempt.ID),
				zap.Error(err),
			)
		}
	}

	if err := e.attRepo.UpdateCallbackPayload(ctx, attempt.ID, evt.RawPayload); err != nil {
		e.logger.Error("failed to store callback payload",
			zap.String("attempt_id", attempt.ID),
			zap.Error(err),
		)
	}

	var newPIStatus domain.PaymentIntentStatus
	var newAttemptStatus domain.AttemptStatus

	switch evt.Status {
	case "succeeded":
		newPIStatus = domain.PISucceeded
		newAttemptStatus = domain.AttemptSucceeded
		telemetry.PaymentSucceeded.Add(ctx, 1, metric.WithAttributes(
			attribute.String("psp", evt.PSP),
		))
	case "failed":
		newPIStatus = domain.PIFailed
		newAttemptStatus = domain.AttemptFailed
		telemetry.PaymentFailed.Add(ctx, 1, metric.WithAttributes(
			attribute.String("psp", evt.PSP),
		))
	default:
		return fmt.Errorf("unknown webhook event status: %s", evt.Status)
	}

	if !IsValidTransition(pi.Status, newPIStatus) {
		e.logger.Warn("invalid state transition from webhook",
			zap.String("from", string(pi.Status)),
			zap.String("to", string(newPIStatus)),
			zap.String("pi_id", pi.ID),
		)
		return nil
	}

	e.logger.Info("transitioning state",
		zap.String("pi_id", pi.ID),
		zap.String("pi_status_from", string(pi.Status)),
		zap.String("pi_status_to", string(newPIStatus)),
		zap.String("attempt_status_from", string(attempt.Status)),
		zap.String("attempt_status_to", string(newAttemptStatus)),
	)

	if err := e.attRepo.UpdateStatus(ctx, attempt.ID, newAttemptStatus); err != nil {
		return fmt.Errorf("update attempt status: %w", err)
	}

	if err := e.piRepo.UpdateStatus(ctx, pi.ID, newPIStatus); err != nil {
		return fmt.Errorf("update pi status: %w", err)
	}

	if newPIStatus == domain.PISucceeded {
		fee := e.computeFee(pi.AmountMinor)
		e.logger.Info("posting ledger for succeeded payment",
			zap.String("pi_id", pi.ID),
			zap.String("merchant_id", pi.MerchantID),
			zap.Int64("amount_minor", pi.AmountMinor),
			zap.String("currency", pi.Currency),
			zap.Int64("platform_fee", fee),
		)
		if err := e.ledger.PostCollection(ctx, pi.MerchantID, pi.ID, attempt.PSP, pi.AmountMinor, pi.Currency, fee); err != nil {
			e.logger.Error("ledger posting failed for succeeded payment",
				zap.String("pi_id", pi.ID),
				zap.Error(err),
			)
			return fmt.Errorf("ledger post: %w", err)
		}
		e.logger.Info("ledger posted for succeeded payment",
			zap.String("pi_id", pi.ID),
		)
	}

	if e.notif != nil {
		notifEvt := notification.NotificationEvent{
			EventType:    fmt.Sprintf("payment.%s", newPIStatus),
			PIID:         pi.ID,
			MerchantID:   pi.MerchantID,
			AmountMinor:  pi.AmountMinor,
			Currency:     pi.Currency,
			PSP:          attempt.PSP,
			PSPReference: attempt.PSPReference,
			Status:       string(newPIStatus),
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
		}
		e.logger.Info("publishing notification",
			zap.String("pi_id", pi.ID),
			zap.String("event_type", notifEvt.EventType),
		)
		if err := e.notif.Publish(ctx, notifEvt); err != nil {
			e.logger.Error("failed to publish notification event",
				zap.String("pi_id", pi.ID),
				zap.Error(err),
			)
		}
	}

	return nil
}

func (e *Engine) computeFee(amountMinor int64) int64 {
	fee := amountMinor * int64(e.feeCfg.Percent) / 100
	if fee < e.feeCfg.MinAmount {
		return e.feeCfg.MinAmount
	}
	return fee
}

func (e *Engine) scheduleDelayedQuery(piID, attemptID, psp string) {
	time.AfterFunc(queryDelay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		pi, err := e.piRepo.GetByID(ctx, piID)
		if err != nil {
			e.logger.Error("delayed query: get pi", zap.String("pi_id", piID), zap.Error(err))
			return
		}
		if pi.Status != domain.PIRequiresAction && pi.Status != domain.PIProcessing {
			return
		}

		attempt, err := e.attRepo.GetByID(ctx, attemptID)
		if err != nil {
			e.logger.Error("delayed query: get attempt", zap.String("attempt_id", attemptID), zap.Error(err))
			return
		}

		conn, ok := e.registry.Get(psp)
		if !ok {
			e.logger.Error("delayed query: connector not found", zap.String("psp", psp))
			return
		}

		e.logger.Info("delayed query: calling GetStatus",
			zap.String("pi_id", piID),
			zap.String("psp_reference", attempt.PSPReference),
		)

		result, err := conn.GetStatus(ctx, attempt.PSPReference)
		if err != nil {
			e.logger.Error("delayed query: GetStatus failed", zap.String("pi_id", piID), zap.Error(err))
			return
		}

		e.logger.Info("delayed query: GetStatus result",
			zap.String("pi_id", piID),
			zap.String("status", result.Status),
		)

		e.applyQueryResult(ctx, pi, attempt, result)
	})
}

func (e *Engine) applyQueryResult(ctx context.Context, pi *domain.PaymentIntent, attempt *domain.Attempt, result connector.StatusResult) {
	var newPIStatus domain.PaymentIntentStatus
	var newAttemptStatus domain.AttemptStatus

	switch result.Status {
	case "succeeded":
		newPIStatus = domain.PISucceeded
		newAttemptStatus = domain.AttemptSucceeded
	case "failed":
		newPIStatus = domain.PIFailed
		newAttemptStatus = domain.AttemptFailed
	case "pending":
		return
	default:
		return
	}

	if !IsValidTransition(pi.Status, newPIStatus) {
		e.logger.Warn("delayed query: invalid transition",
			zap.String("from", string(pi.Status)),
			zap.String("to", string(newPIStatus)),
			zap.String("pi_id", pi.ID),
		)
		return
	}

	if err := e.attRepo.UpdateStatus(ctx, attempt.ID, newAttemptStatus); err != nil {
		e.logger.Error("delayed query: update attempt status", zap.String("attempt_id", attempt.ID), zap.Error(err))
		return
	}

	if err := e.piRepo.UpdateStatus(ctx, pi.ID, newPIStatus); err != nil {
		e.logger.Error("delayed query: update pi status", zap.String("pi_id", pi.ID), zap.Error(err))
		return
	}

	e.logger.Info("delayed query: status updated",
		zap.String("pi_id", pi.ID),
		zap.String("pi_status", string(newPIStatus)),
		zap.String("attempt_status", string(newAttemptStatus)),
	)

	if newPIStatus == domain.PISucceeded {
		fee := e.computeFee(pi.AmountMinor)
		if err := e.ledger.PostCollection(ctx, pi.MerchantID, pi.ID, attempt.PSP, pi.AmountMinor, pi.Currency, fee); err != nil {
			e.logger.Error("delayed query: ledger post failed", zap.String("pi_id", pi.ID), zap.Error(err))
			return
		}
	}

	if e.notif != nil {
		evt := notification.NotificationEvent{
			EventType:    fmt.Sprintf("payment.%s", newPIStatus),
			PIID:         pi.ID,
			MerchantID:   pi.MerchantID,
			AmountMinor:  pi.AmountMinor,
			Currency:     pi.Currency,
			PSP:          attempt.PSP,
			PSPReference: attempt.PSPReference,
			Status:       string(newPIStatus),
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
		}
		if err := e.notif.Publish(ctx, evt); err != nil {
			e.logger.Error("delayed query: notification publish failed", zap.String("pi_id", pi.ID), zap.Error(err))
		}
	}
}
