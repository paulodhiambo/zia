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
	orc    *orchestrator.Engine
	js     nats.JetStreamContext
	logger *zap.Logger
}

func NewProcessor(
	orc *orchestrator.Engine,
	js nats.JetStreamContext,
	logger *zap.Logger,
) *Processor {
	return &Processor{
		orc:    orc,
		js:     js,
		logger: logger,
	}
}

func (p *Processor) Publish(ctx context.Context, event connector.WebhookEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal webhook event: %w", err)
	}

	msg := &nats.Msg{
		Subject: "zia.webhook.received",
		Data:    data,
		Header:  make(nats.Header),
	}

	if msg.Header == nil {
		msg.Header = make(nats.Header)
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

			if err := msg.Ack(); err != nil {
				p.logger.Error("nats ack failed", zap.Error(err))
			}
		}
	}()

	return nil
}
