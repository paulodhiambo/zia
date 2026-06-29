package api

import (
	"io"
	"net/http"
	"time"

	"zia/internal/connector"
	"zia/internal/domain"
	"zia/internal/repository"
	"zia/internal/webhook"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

func truncateBytes(b []byte, maxLen int) []byte {
	if len(b) <= maxLen {
		return b
	}
	return b[:maxLen]
}

type WebhookHandler struct {
	registry  *connector.Registry
	whRepo    repository.WebhookEventRepository
	dedup     *webhook.DedupStore
	publisher *webhook.Processor
	logger    *zap.Logger
}

func NewWebhookHandler(
	registry *connector.Registry,
	whRepo repository.WebhookEventRepository,
	dedup *webhook.DedupStore,
	publisher *webhook.Processor,
	logger *zap.Logger,
) *WebhookHandler {
	return &WebhookHandler{
		registry:  registry,
		whRepo:    whRepo,
		dedup:     dedup,
		publisher: publisher,
		logger:    logger,
	}
}

func (h *WebhookHandler) Ingest(w http.ResponseWriter, r *http.Request) {
	psp := r.PathValue("psp")
	if psp == "" {
		http.Error(w, "unknown psp", http.StatusNotFound)
		return
	}

	conn, ok := h.registry.Get(psp)
	if !ok {
		http.Error(w, "unknown psp", http.StatusNotFound)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		h.logger.Error("failed to read webhook body", zap.Error(err))
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	event, err := conn.ParseWebhook(r.Context(), headers, body)
	if err != nil {
		h.logger.Error("webhook parse failed",
			zap.String("psp", psp),
			zap.ByteString("body", truncateBytes(body, 1000)),
			zap.Error(err),
		)
		w.WriteHeader(http.StatusOK)
		return
	}

	now := time.Now().UTC()
	wh := &domain.WebhookEvent{
		ID:               uuid.New().String(),
		PSP:              event.PSP,
		EventType:        event.EventType,
		PSPReference:     event.PSPReference,
		DedupKey:         event.DedupKey,
		Payload:          event.RawPayload,
		ProcessingStatus: domain.WebhookReceived,
		ReceivedAt:       now,
	}

	if err := h.whRepo.Create(r.Context(), wh); err != nil {
		h.logger.Error("failed to persist webhook event (may be duplicate)", zap.Error(err))
	} else {
		if err := h.dedup.Mark(r.Context(), event.DedupKey); err != nil {
			h.logger.Error("failed to mark dedup", zap.Error(err))
		}
	}

	h.logger.Info("webhook received",
		zap.String("psp", psp),
		zap.String("event_type", event.EventType),
		zap.String("psp_reference", event.PSPReference),
		zap.String("dedup_key", event.DedupKey),
	)

	// Acknowledge the PSP immediately — they retry on non-2xx or slow responses.
	w.WriteHeader(http.StatusOK)

	// Publish to NATS for async processing by the worker. Fall back to synchronous
	// processing when NATS is unavailable (e.g. local dev without the worker).
	if err := h.publisher.Publish(r.Context(), event); err != nil {
		h.logger.Warn("nats unavailable, processing webhook synchronously",
			zap.String("psp", psp),
			zap.Error(err),
		)
		h.publisher.HandleEventAndDispatch(r.Context(), event, wh.ID)
	}
}
