package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"zia/internal/connector"
	"zia/internal/domain"
	"zia/internal/repository"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	maxRetries     = 3
	retryDelayBase = 30 * time.Second
)

type Dispatcher struct {
	attRepo    repository.AttemptRepository
	piRepo     repository.PaymentIntentRepository
	epRepo     repository.WebhookEndpointRepository
	delRepo    repository.WebhookDeliveryRepository
	httpClient *http.Client
	logger     *zap.Logger
}

func NewDispatcher(
	attRepo repository.AttemptRepository,
	piRepo repository.PaymentIntentRepository,
	epRepo repository.WebhookEndpointRepository,
	delRepo repository.WebhookDeliveryRepository,
	logger *zap.Logger,
) *Dispatcher {
	return &Dispatcher{
		attRepo: attRepo,
		piRepo:  piRepo,
		epRepo:  epRepo,
		delRepo: delRepo,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

type dispatchPayload struct {
	Event     string `json:"event"`
	PIID      string `json:"piId"`
	Amount    int64  `json:"amount"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	PSP       string `json:"psp"`
	Timestamp string `json:"timestamp"`
}

func (d *Dispatcher) Dispatch(ctx context.Context, event connector.WebhookEvent, merchantID string, whEventID string) {
	eps, err := d.epRepo.ListByMerchant(ctx, merchantID)
	if err != nil {
		d.logger.Error("dispatch: list endpoints", zap.String("merchant_id", merchantID), zap.Error(err))
		return
	}

	if len(eps) == 0 {
		return
	}

	// Determine the event type for the payload
	eventType := fmt.Sprintf("payment.%s", event.Status)

	payload := dispatchPayload{
		Event:     eventType,
		PIID:      "",
		Amount:    event.Amount,
		Currency:  event.Currency,
		Status:    event.Status,
		PSP:       event.PSP,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	body, _ := json.Marshal(payload)

	for _, ep := range eps {
		if ep.Status != "active" {
			continue
		}
		d.deliver(ctx, ep, whEventID, body, 1)
	}
}

func (d *Dispatcher) deliver(ctx context.Context, ep domain.WebhookEndpoint, whEventID string, body []byte, attempt int) {
	delID := uuid.New().String()
	now := time.Now().UTC()

	var nextRetryAt *time.Time
	if attempt < maxRetries {
		t := now.Add(retryDelayBase * time.Duration(attempt))
		nextRetryAt = &t
	}

	var weID *string
	if whEventID != "" {
		weID = &whEventID
	}

	del := &domain.WebhookDelivery{
		ID:             delID,
		WebhookEventID: weID,
		EndpointID:     ep.ID,
		URL:            ep.URL,
		Status:         domain.DeliveryPending,
		RequestHeaders: nil,
		RequestBody:    body,
		Attempt:        attempt,
		MaxAttempts:    maxRetries,
		NextRetryAt:    nextRetryAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := d.delRepo.Create(ctx, del); err != nil {
		d.logger.Error("dispatch: create delivery record", zap.String("endpoint_id", ep.ID), zap.Error(err))
		return
	}

	d.sendAndRecord(ctx, del, body)
}

func (d *Dispatcher) sendAndRecord(ctx context.Context, del *domain.WebhookDelivery, body []byte) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, del.URL, bytes.NewReader(body))
	if err != nil {
		d.logger.Error("dispatch: create request", zap.String("delivery_id", del.ID), zap.Error(err))
		d.failDelivery(ctx, del.ID, 0, nil, time.Since(start))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Zia-Webhook/1.0")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		d.logger.Warn("dispatch: request failed",
			zap.String("delivery_id", del.ID),
			zap.String("url", del.URL),
			zap.Error(err),
		)
		d.failDelivery(ctx, del.ID, 0, nil, time.Since(start))
		return
	}
	defer resp.Body.Close()

	var respBody []byte
	if resp.ContentLength > 0 && resp.ContentLength < 1<<20 {
		buf := make([]byte, resp.ContentLength)
		resp.Body.Read(buf)
		respBody = buf
	}

	dur := time.Since(start)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		d.logger.Info("dispatch: delivered",
			zap.String("delivery_id", del.ID),
			zap.Int("status", resp.StatusCode),
			zap.Duration("duration", dur),
		)
		if err := d.delRepo.UpdateStatus(ctx, del.ID, domain.DeliveryDelivered, resp.StatusCode, respBody, int(dur.Milliseconds())); err != nil {
			d.logger.Error("dispatch: update delivery status", zap.String("delivery_id", del.ID), zap.Error(err))
		}
	} else {
		d.logger.Warn("dispatch: non-2xx response",
			zap.String("delivery_id", del.ID),
			zap.Int("status", resp.StatusCode),
			zap.Duration("duration", dur),
		)
		d.failDelivery(ctx, del.ID, resp.StatusCode, respBody, dur)
	}
}

func (d *Dispatcher) failDelivery(ctx context.Context, delID string, respStatus int, respBody []byte, dur time.Duration) {
	if err := d.delRepo.UpdateStatus(ctx, delID, domain.DeliveryFailed, respStatus, respBody, int(dur.Milliseconds())); err != nil {
		d.logger.Error("dispatch: fail delivery", zap.String("delivery_id", delID), zap.Error(err))
	}
}

func (d *Dispatcher) Retry(ctx context.Context, deliveryID string) error {
	del, err := d.delRepo.GetByID(ctx, deliveryID)
	if err != nil {
		return fmt.Errorf("get delivery: %w", err)
	}
	if del.Status != domain.DeliveryFailed {
		return fmt.Errorf("delivery %s is not in failed state", deliveryID)
	}

	newAttempt := del.Attempt + 1
	var whEventID string
	if del.WebhookEventID != nil {
		whEventID = *del.WebhookEventID
	}
	d.deliver(ctx, domain.WebhookEndpoint{
		ID:  del.EndpointID,
		URL: del.URL,
	}, whEventID, del.RequestBody, newAttempt)
	return nil
}
