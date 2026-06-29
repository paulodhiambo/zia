package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"zia/internal/connector"
	"zia/internal/orchestrator"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/zap"
)

type Processor struct {
	orc        *orchestrator.Engine
	dispatcher *Dispatcher
	js         nats.JetStreamContext
	logger     *zap.Logger
}

func NewProcessor(
	orc *orchestrator.Engine,
	dispatcher *Dispatcher,
	js nats.JetStreamContext,
	logger *zap.Logger,
) *Processor {
	return &Processor{
		orc:        orc,
		dispatcher: dispatcher,
		js:         js,
		logger:     logger,
	}
}

func (p *Processor) HandleEvent(ctx context.Context, event connector.WebhookEvent) error {
	return p.orc.HandleWebhookEvent(ctx, event)
}

func (p *Processor) HandleEventAndDispatch(ctx context.Context, event connector.WebhookEvent, whEventID string) {
	if err := p.orc.HandleWebhookEvent(ctx, event); err != nil {
		p.logger.Error("process webhook event",
			zap.String("psp", event.PSP),
			zap.String("psp_reference", event.PSPReference),
			zap.Error(err),
		)
		return
	}

	if p.dispatcher != nil {
		// Look up merchant ID from the attempt to dispatch to endpoints.
		attempt, err := p.orc.GetAttemptByPSPReference(ctx, event.PSP, event.PSPReference)
		if err != nil {
			p.logger.Error("dispatch: lookup attempt", zap.String("psp", event.PSP), zap.String("psp_reference", event.PSPReference), zap.Error(err))
			return
		}
		pi, err := p.orc.GetPaymentIntent(ctx, attempt.PaymentIntentID)
		if err != nil {
			p.logger.Error("dispatch: lookup pi", zap.String("pi_id", attempt.PaymentIntentID), zap.Error(err))
			return
		}
		p.dispatcher.Dispatch(ctx, event, pi.MerchantID, whEventID)
	}
}

func (p *Processor) CanPublish() bool {
	return p.js != nil
}

func (p *Processor) Publish(ctx context.Context, event connector.WebhookEvent) error {
	if p.js == nil {
		return fmt.Errorf("jetstream not available")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal webhook event: %w", err)
	}

	msg := &nats.Msg{
		Subject: "zia.webhook.received",
		Data:    data,
		Header:  make(nats.Header),
	}
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(msg.Header))

	_, err = p.js.PublishMsg(msg)
	if err != nil {
		return fmt.Errorf("publish to jetstream: %w", err)
	}
	return nil
}

func (p *Processor) StartConsumer(ctx context.Context) error {
	sub, err := p.js.SubscribeSync("zia.webhook.received", nats.Durable("zia-webhook-worker"))
	if err != nil {
		return fmt.Errorf("subscribe to zia.webhook.received: %w", err)
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
					p.logger.Error("nats next msg error", zap.Error(err))
				}
				continue
			}

			ctx := otel.GetTextMapPropagator().Extract(context.Background(),
				propagation.HeaderCarrier(msg.Header))

			var event connector.WebhookEvent
			if err := json.Unmarshal(msg.Data, &event); err != nil {
				p.logger.Error("failed to unmarshal webhook event",
					zap.Error(err),
					zap.ByteString("data", msg.Data),
				)
				if err := msg.Nak(); err != nil {
					p.logger.Error("nats nak failed", zap.Error(err))
				}
				continue
			}

			if err := p.orc.HandleWebhookEvent(ctx, event); err != nil {
				p.logger.Error("failed to process webhook event",
					zap.String("psp", event.PSP),
					zap.String("psp_reference", event.PSPReference),
					zap.Error(err),
				)
				if err := msg.Nak(); err != nil {
					p.logger.Error("nats nak failed", zap.Error(err))
				}
				continue
			}

			if p.dispatcher != nil {
				attempt, err := p.orc.GetAttemptByPSPReference(ctx, event.PSP, event.PSPReference)
				if err != nil {
					p.logger.Error("consumer dispatch: lookup attempt", zap.Error(err))
				} else {
					pi, err := p.orc.GetPaymentIntent(ctx, attempt.PaymentIntentID)
					if err != nil {
						p.logger.Error("consumer dispatch: lookup pi", zap.Error(err))
					} else {
						// We don't have whEventID here — pass empty so dispatcher skips
						// delivery record creation; merchant endpoints will only
						// get notified on the sync path.
						p.dispatcher.Dispatch(ctx, event, pi.MerchantID, "")
					}
				}
			}

			if err := msg.Ack(); err != nil {
				p.logger.Error("nats ack failed", zap.Error(err))
			}
		}
	}()

	return nil
}
