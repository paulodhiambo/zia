package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"zia/internal/domain"
	"zia/internal/repository"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

type SettlementConfig struct {
	WebhookURL string `json:"webhook_url,omitempty"`
}

type Dispatcher struct {
	merchantRepo repository.MerchantRepository
	notifRepo    repository.NotificationRepository
	http         *http.Client
	js           nats.JetStreamContext
	logger       *zap.Logger
}

func NewDispatcher(merchantRepo repository.MerchantRepository, notifRepo repository.NotificationRepository, js nats.JetStreamContext, logger *zap.Logger) *Dispatcher {
	return &Dispatcher{
		merchantRepo: merchantRepo,
		notifRepo:    notifRepo,
		http:         &http.Client{Timeout: 15 * time.Second},
		js:           js,
		logger:       logger,
	}
}

type NotificationEvent struct {
	EventType    string `json:"event_type"`
	PIID         string `json:"pi_id"`
	MerchantID   string `json:"merchant_id"`
	AmountMinor  int64  `json:"amount_minor"`
	Currency     string `json:"currency"`
	PSP          string `json:"psp"`
	PSPReference string `json:"psp_reference"`
	Status       string `json:"status"`
	Timestamp    string `json:"timestamp"`
}

func (d *Dispatcher) Publish(ctx context.Context, evt NotificationEvent) error {
	data, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("marshal notification event: %w", err)
	}

	subject := fmt.Sprintf("zia.notification.%s", evt.Status)
	msg := &nats.Msg{
		Subject: subject,
		Data:    data,
		Header:  make(nats.Header),
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))

	_, err = d.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("publish notification: %w", err)
	}
	return nil
}

func (d *Dispatcher) StartConsumer(ctx context.Context) error {
	sub, err := d.js.SubscribeSync("zia.notification.>", nats.Durable("zia-notification-dispatcher"))
	if err != nil {
		return fmt.Errorf("subscribe to zia.notification.>: %w", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			msg, err := sub.NextMsg(5 * time.Second)
			if err != nil {
				if err != nats.ErrTimeout {
					d.logger.Error("nats next msg error in notification", zap.Error(err))
				}
				continue
			}

			ctx := otel.GetTextMapPropagator().Extract(context.Background(),
				propagation.HeaderCarrier(msg.Header))

			var evt NotificationEvent
			if err := json.Unmarshal(msg.Data, &evt); err != nil {
				d.logger.Error("failed to unmarshal notification event",
					zap.Error(err),
					zap.ByteString("data", msg.Data),
				)
				if err := msg.Nak(); err != nil {
					d.logger.Error("nats nak failed", zap.Error(err))
				}
				continue
			}

			if err := d.processNotification(ctx, evt); err != nil {
				d.logger.Error("failed to process notification",
					zap.String("event_type", evt.EventType),
					zap.String("pi_id", evt.PIID),
					zap.Error(err),
				)
				if err := msg.Nak(); err != nil {
					d.logger.Error("nats nak failed", zap.Error(err))
				}
				continue
			}

			if err := msg.Ack(); err != nil {
				d.logger.Error("nats ack failed", zap.Error(err))
			}
		}
	}()

	return nil
}

func (d *Dispatcher) processNotification(ctx context.Context, evt NotificationEvent) error {
	merchant, err := d.merchantRepo.GetByID(ctx, evt.MerchantID)
	if err != nil {
		return fmt.Errorf("lookup merchant %s: %w", evt.MerchantID, err)
	}

	n := d.notificationFromEvent(evt)
	if err := d.notifRepo.Create(ctx, n); err != nil {
		d.logger.Error("failed to persist notification", zap.String("pi_id", evt.PIID), zap.Error(err))
	}

	webhookURL := webhookURLFromConfig(merchant)
	if webhookURL == "" {
		return nil
	}

	payload, _ := json.Marshal(map[string]any{
		"event":      evt.EventType,
		"pi_id":      evt.PIID,
		"amount":     evt.AmountMinor,
		"currency":   evt.Currency,
		"psp":        evt.PSPReference,
		"status":     evt.Status,
		"timestamp":  evt.Timestamp,
	})

	return d.sendWithRetry(ctx, webhookURL, payload)
}

func (d *Dispatcher) notificationFromEvent(evt NotificationEvent) *domain.Notification {
	var tone, title, body, category string
	switch {
	case evt.Status == "succeeded":
		tone = "success"
		title = "Payment Successful"
		body = fmt.Sprintf("%s payment of %d %s was successful", evt.PSP, evt.AmountMinor, evt.Currency)
		category = "payment"
	case evt.Status == "failed":
		tone = "error"
		title = "Payment Failed"
		body = fmt.Sprintf("%s payment of %d %s failed", evt.PSP, evt.AmountMinor, evt.Currency)
		category = "payment"
	default:
		tone = "info"
		title = fmt.Sprintf("Payment %s", evt.Status)
		body = fmt.Sprintf("%s payment of %d %s is %s", evt.PSP, evt.AmountMinor, evt.Currency, evt.Status)
		category = "payment"
	}

	return &domain.Notification{
		ID:         uuid.New().String(),
		MerchantID: evt.MerchantID,
		Tone:       tone,
		Title:      title,
		Body:       body,
		Category:   category,
		Unread:     true,
		CreatedAt:  time.Now().UTC(),
	}
}

func (d *Dispatcher) sendWithRetry(ctx context.Context, url string, payload []byte) error {
	maxRetries := 3
	backoff := []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "Zia-Webhook/1.0")

		resp, err := d.http.Do(req)
		if err != nil {
			if attempt < maxRetries {
				d.logger.Warn("webhook send failed, retrying",
					zap.String("url", url),
					zap.Int("attempt", attempt),
					zap.Error(err),
				)
				time.Sleep(backoff[attempt])
				continue
			}
			return fmt.Errorf("webhook send failed after retries: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		if attempt < maxRetries {
			d.logger.Warn("webhook non-2xx, retrying",
				zap.String("url", url),
				zap.Int("status", resp.StatusCode),
				zap.Int("attempt", attempt),
			)
			time.Sleep(backoff[attempt])
			continue
		}

		return fmt.Errorf("webhook non-2xx after retries: %d", resp.StatusCode)
	}

	return nil
}

func webhookURLFromConfig(m *domain.Merchant) string {
	if m.SettlementConfig == nil {
		return ""
	}
	var cfg SettlementConfig
	if err := json.Unmarshal(m.SettlementConfig, &cfg); err != nil {
		return ""
	}
	return cfg.WebhookURL
}
