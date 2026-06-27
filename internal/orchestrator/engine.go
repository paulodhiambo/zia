package orchestrator

import (
	"context"
	"fmt"
	"time"

	"zia/internal/connector"
	"zia/internal/domain"
	"zia/internal/idempotency"
	"zia/internal/ledger"
	"zia/internal/repository"
	"zia/internal/risk"
	"zia/internal/routing"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

type Engine struct {
	piRepo      repository.PaymentIntentRepository
	attRepo     repository.AttemptRepository
	registry    *connector.Registry
	router      *routing.Engine
	risk        *risk.Engine
	idempotency *idempotency.Store
	logger      *zap.Logger
	ledger      *ledger.Engine
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
) *Engine {
	return &Engine{
		piRepo:      piRepo,
		attRepo:     attRepo,
		registry:    registry,
		router:      router,
		risk:        risk,
		idempotency: idempotency,
		logger:      logger,
		ledger:      ledger,
	}
}

type CreatePIRequest struct {
	MerchantID    string
	AmountMinor   int64
	Currency      string
	Method        domain.PaymentMethod
	CustomerRef   string
	CustomerPhone string
	CustomerEmail string
	IdempotencyKey string
	Metadata      []byte
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
			return &CreatePIResult{PaymentIntent: pi}, nil
		}
	}

	if err := e.risk.Evaluate(ctx, risk.Request{
		MerchantID:    req.MerchantID,
		AmountMinor:   req.AmountMinor,
		Currency:      req.Currency,
		Method:        string(req.Method),
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: req.CustomerEmail,
	}); err != nil {
		return nil, fmt.Errorf("risk check: %w", err)
	}

	route, err := e.router.Route(ctx, routing.RouteRequest{
		MerchantID:  req.MerchantID,
		Currency:    req.Currency,
		Method:      string(req.Method),
		AmountMinor: req.AmountMinor,
	})
	if err != nil {
		return nil, fmt.Errorf("routing: %w", err)
	}

	conn, ok := e.registry.Get(route.Primary)
	if !ok {
		return nil, fmt.Errorf("connector %s not found in registry", route.Primary)
	}

	now := time.Now().UTC()
	pi := &domain.PaymentIntent{
		ID:            "pi_" + uuid.New().String(),
		MerchantID:    req.MerchantID,
		AmountMinor:   req.AmountMinor,
		Currency:      req.Currency,
		Status:        domain.PICreated,
		Method:        req.Method,
		CustomerRef:   req.CustomerRef,
		CustomerPhone: req.CustomerPhone,
		CustomerEmail: req.CustomerEmail,
		IdempotencyKey: req.IdempotencyKey,
		Metadata:      req.Metadata,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	expiresAt := now.Add(30 * time.Minute)
	pi.ExpiresAt = &expiresAt

	if err := e.piRepo.Create(ctx, pi); err != nil {
		return nil, fmt.Errorf("save payment intent: %w", err)
	}

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

	colResult, err := conn.InitiateCollection(ctx, colReq)
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
		return &CreatePIResult{PaymentIntent: pi}, nil
	}

	attemptID := uuid.New().String()
	attemptStatus := domain.AttemptRequiresAction
	if colResult.Status == "processing" {
		attemptStatus = domain.AttemptProcessing
	}

	attempt := &domain.Attempt{
		ID:              attemptID,
		PaymentIntentID: pi.ID,
		PSP:             route.Primary,
		PSPReference:    colResult.PSPReference,
		Status:          attemptStatus,
		SequenceNo:      1,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	if err := e.attRepo.Create(ctx, attempt); err != nil {
		return nil, fmt.Errorf("save attempt: %w", err)
	}

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

	return &CreatePIResult{
		PaymentIntent: pi,
		NextAction:    colResult.NextAction,
	}, nil
}

func (e *Engine) GetPaymentIntent(ctx context.Context, id string) (*domain.PaymentIntent, error) {
	return e.piRepo.GetByID(ctx, id)
}

func (e *Engine) HandleWebhookEvent(ctx context.Context, evt connector.WebhookEvent) error {
	attempt, err := e.attRepo.GetByPSPReference(ctx, evt.PSP, evt.PSPReference)
	if err != nil {
		return fmt.Errorf("lookup attempt by psp ref: %w", err)
	}

	pi, err := e.piRepo.GetByID(ctx, attempt.PaymentIntentID)
	if err != nil {
		return fmt.Errorf("lookup pi: %w", err)
	}

	var newPIStatus domain.PaymentIntentStatus
	var newAttemptStatus domain.AttemptStatus

	switch evt.Status {
	case "succeeded":
		newPIStatus = domain.PISucceeded
		newAttemptStatus = domain.AttemptSucceeded
	case "failed":
		newPIStatus = domain.PIFailed
		newAttemptStatus = domain.AttemptFailed
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

	if err := e.attRepo.UpdateStatus(ctx, attempt.ID, newAttemptStatus); err != nil {
		return fmt.Errorf("update attempt status: %w", err)
	}

	if err := e.piRepo.UpdateStatus(ctx, pi.ID, newPIStatus); err != nil {
		return fmt.Errorf("update pi status: %w", err)
	}

	if newPIStatus == domain.PISucceeded {
		fee := computePlatformFee(pi.AmountMinor)
		if err := e.ledger.PostCollection(ctx, pi.MerchantID, pi.ID, attempt.PSP, pi.AmountMinor, pi.Currency, fee); err != nil {
			e.logger.Error("ledger posting failed for succeeded payment",
				zap.String("pi_id", pi.ID),
				zap.Error(err),
			)
			return fmt.Errorf("ledger post: %w", err)
		}
	}

	return nil
}

func computePlatformFee(amountMinor int64) int64 {
	fee := amountMinor * 5 / 100
	if fee < 100 {
		return 100
	}
	return fee
}
