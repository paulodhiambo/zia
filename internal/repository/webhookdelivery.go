package repository

import (
	"context"
	"time"

	"zia/internal/domain"
)

type WebhookDeliveryRepository interface {
	Create(ctx context.Context, d *domain.WebhookDelivery) error
	GetByID(ctx context.Context, id string) (*domain.WebhookDelivery, error)
	ListByEvent(ctx context.Context, webhookEventID string) ([]domain.WebhookDelivery, error)
	UpdateStatus(ctx context.Context, id string, status domain.WebhookDeliveryStatus, responseStatus int, responseBody []byte, durationMs int) error
	ScheduleRetry(ctx context.Context, id string, nextRetryAt time.Time) error
	DueForRetry(ctx context.Context, now time.Time, limit int) ([]domain.WebhookDelivery, error)
}

type webhookDeliveryRepo struct {
	db DBTX
}

func NewWebhookDeliveryRepo(db DBTX) WebhookDeliveryRepository {
	return &webhookDeliveryRepo{db: db}
}

func (r *webhookDeliveryRepo) Create(ctx context.Context, d *domain.WebhookDelivery) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO webhook_deliveries (id, webhook_event_id, endpoint_id, url, status,
			request_headers, request_body, attempt, max_attempts, next_retry_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		d.ID, d.WebhookEventID, d.EndpointID, d.URL, d.Status,
		d.RequestHeaders, d.RequestBody, d.Attempt, d.MaxAttempts, d.NextRetryAt,
		d.CreatedAt, d.UpdatedAt)
	return err
}

func (r *webhookDeliveryRepo) GetByID(ctx context.Context, id string) (*domain.WebhookDelivery, error) {
	d := &domain.WebhookDelivery{}
	err := r.db.QueryRow(ctx, `
		SELECT id, webhook_event_id, endpoint_id, url, status,
			request_headers, request_body, response_status, response_body, duration_ms,
			attempt, max_attempts, next_retry_at, created_at, updated_at
		FROM webhook_deliveries WHERE id = $1`, id).Scan(
		&d.ID, &d.WebhookEventID, &d.EndpointID, &d.URL, &d.Status,
		&d.RequestHeaders, &d.RequestBody, &d.ResponseStatus, &d.ResponseBody, &d.DurationMs,
		&d.Attempt, &d.MaxAttempts, &d.NextRetryAt, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (r *webhookDeliveryRepo) ListByEvent(ctx context.Context, webhookEventID string) ([]domain.WebhookDelivery, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, webhook_event_id, endpoint_id, url, status,
			request_headers, request_body, response_status, response_body, duration_ms,
			attempt, max_attempts, next_retry_at, created_at, updated_at
		FROM webhook_deliveries WHERE webhook_event_id = $1
		ORDER BY created_at ASC`, webhookEventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ds []domain.WebhookDelivery
	for rows.Next() {
		var d domain.WebhookDelivery
		if err := rows.Scan(
			&d.ID, &d.WebhookEventID, &d.EndpointID, &d.URL, &d.Status,
			&d.RequestHeaders, &d.RequestBody, &d.ResponseStatus, &d.ResponseBody, &d.DurationMs,
			&d.Attempt, &d.MaxAttempts, &d.NextRetryAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		ds = append(ds, d)
	}
	return ds, nil
}

func (r *webhookDeliveryRepo) UpdateStatus(ctx context.Context, id string, status domain.WebhookDeliveryStatus, responseStatus int, responseBody []byte, durationMs int) error {
	_, err := r.db.Exec(ctx,
		`UPDATE webhook_deliveries SET status = $1, response_status = $2, response_body = $3,
			duration_ms = $4, updated_at = now() WHERE id = $5`,
		status, responseStatus, responseBody, durationMs, id)
	return err
}

func (r *webhookDeliveryRepo) ScheduleRetry(ctx context.Context, id string, nextRetryAt time.Time) error {
	_, err := r.db.Exec(ctx,
		`UPDATE webhook_deliveries SET next_retry_at = $1, updated_at = now() WHERE id = $2`,
		nextRetryAt, id)
	return err
}

func (r *webhookDeliveryRepo) DueForRetry(ctx context.Context, now time.Time, limit int) ([]domain.WebhookDelivery, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, webhook_event_id, endpoint_id, url, status,
			request_headers, request_body, response_status, response_body, duration_ms,
			attempt, max_attempts, next_retry_at, created_at, updated_at
		FROM webhook_deliveries
		WHERE status = 'failed' AND next_retry_at IS NOT NULL AND next_retry_at <= $1
		ORDER BY next_retry_at ASC LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ds []domain.WebhookDelivery
	for rows.Next() {
		var d domain.WebhookDelivery
		if err := rows.Scan(
			&d.ID, &d.WebhookEventID, &d.EndpointID, &d.URL, &d.Status,
			&d.RequestHeaders, &d.RequestBody, &d.ResponseStatus, &d.ResponseBody, &d.DurationMs,
			&d.Attempt, &d.MaxAttempts, &d.NextRetryAt, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		ds = append(ds, d)
	}
	return ds, nil
}
