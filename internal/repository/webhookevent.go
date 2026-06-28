package repository

import (
	"context"

	"zia/internal/domain"
)

type WebhookEventRepository interface {
	Create(ctx context.Context, e *domain.WebhookEvent) error
	GetByDedupKey(ctx context.Context, dedupKey string) (*domain.WebhookEvent, error)
	UpdateProcessingStatus(ctx context.Context, id string, status domain.WebhookProcessingStatus) error
	ListByMerchant(ctx context.Context, merchantID string, limit int) ([]domain.WebhookEvent, error)
}

type webhookEventRepo struct {
	db DBTX
}

func NewWebhookEvent(db DBTX) WebhookEventRepository {
	return &webhookEventRepo{db: db}
}

func (r *webhookEventRepo) Create(ctx context.Context, e *domain.WebhookEvent) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO webhook_events (id, psp, event_type, psp_reference, dedup_key,
			payload, processing_status, received_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.PSP, e.EventType, e.PSPReference, e.DedupKey,
		e.Payload, e.ProcessingStatus, e.ReceivedAt)
	return err
}

func (r *webhookEventRepo) GetByDedupKey(ctx context.Context, dedupKey string) (*domain.WebhookEvent, error) {
	e := &domain.WebhookEvent{}
	err := r.db.QueryRow(ctx, `
		SELECT id, psp, event_type, psp_reference, dedup_key, payload,
			processing_status, received_at
		FROM webhook_events WHERE dedup_key = $1`, dedupKey).Scan(
		&e.ID, &e.PSP, &e.EventType, &e.PSPReference, &e.DedupKey,
		&e.Payload, &e.ProcessingStatus, &e.ReceivedAt)
	if err != nil {
		return nil, err
	}
	return e, nil
}

func (r *webhookEventRepo) UpdateProcessingStatus(ctx context.Context, id string, status domain.WebhookProcessingStatus) error {
	_, err := r.db.Exec(ctx,
		`UPDATE webhook_events SET processing_status = $1 WHERE id = $2`,
		status, id)
	return err
}

func (r *webhookEventRepo) ListByMerchant(ctx context.Context, merchantID string, limit int) ([]domain.WebhookEvent, error) {
	rows, err := r.db.Query(ctx, `
		SELECT e.id, e.psp, e.event_type, e.psp_reference, e.dedup_key,
		       e.payload, e.processing_status, e.received_at
		FROM webhook_events e
		JOIN attempts a ON a.psp_reference = e.psp_reference
		JOIN payment_intents pi ON pi.id = a.payment_intent_id
		WHERE pi.merchant_id = $1
		ORDER BY e.received_at DESC
		LIMIT $2`, merchantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var evts []domain.WebhookEvent
	for rows.Next() {
		var e domain.WebhookEvent
		if err := rows.Scan(&e.ID, &e.PSP, &e.EventType, &e.PSPReference, &e.DedupKey,
			&e.Payload, &e.ProcessingStatus, &e.ReceivedAt); err != nil {
			return nil, err
		}
		evts = append(evts, e)
	}
	return evts, nil
}
