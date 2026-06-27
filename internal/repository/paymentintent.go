package repository

import (
	"context"

	"zia/internal/domain"
)

type PaymentIntentRepository interface {
	Create(ctx context.Context, pi *domain.PaymentIntent) error
	GetByID(ctx context.Context, id string) (*domain.PaymentIntent, error)
	UpdateStatus(ctx context.Context, id string, status domain.PaymentIntentStatus) error
	UpdateStatusSafe(ctx context.Context, id string, from, to domain.PaymentIntentStatus) (bool, error)
	ListByMerchant(ctx context.Context, merchantID string, limit, offset int) ([]domain.PaymentIntent, error)
}

type paymentIntentRepo struct {
	db DBTX
}

func NewPaymentIntent(db DBTX) PaymentIntentRepository {
	return &paymentIntentRepo{db: db}
}

func (r *paymentIntentRepo) Create(ctx context.Context, pi *domain.PaymentIntent) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO payment_intents (id, merchant_id, amount_minor, currency, status, method,
			customer_ref, customer_phone, customer_email, idempotency_key, metadata, expires_at,
			created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		pi.ID, pi.MerchantID, pi.AmountMinor, pi.Currency, pi.Status, pi.Method,
		pi.CustomerRef, pi.CustomerPhone, pi.CustomerEmail, pi.IdempotencyKey, pi.Metadata,
		pi.ExpiresAt, pi.CreatedAt, pi.UpdatedAt)
	return err
}

func (r *paymentIntentRepo) GetByID(ctx context.Context, id string) (*domain.PaymentIntent, error) {
	pi := &domain.PaymentIntent{}
	err := r.db.QueryRow(ctx, `
		SELECT id, merchant_id, amount_minor, currency, status, method,
			customer_ref, customer_phone, customer_email, idempotency_key, metadata,
			expires_at, created_at, updated_at
		FROM payment_intents WHERE id = $1`, id).Scan(
		&pi.ID, &pi.MerchantID, &pi.AmountMinor, &pi.Currency, &pi.Status, &pi.Method,
		&pi.CustomerRef, &pi.CustomerPhone, &pi.CustomerEmail, &pi.IdempotencyKey,
		&pi.Metadata, &pi.ExpiresAt, &pi.CreatedAt, &pi.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return pi, nil
}

func (r *paymentIntentRepo) UpdateStatus(ctx context.Context, id string, status domain.PaymentIntentStatus) error {
	_, err := r.db.Exec(ctx,
		`UPDATE payment_intents SET status = $1, updated_at = now() WHERE id = $2`,
		status, id)
	return err
}

func (r *paymentIntentRepo) UpdateStatusSafe(ctx context.Context, id string, from, to domain.PaymentIntentStatus) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE payment_intents SET status = $1, updated_at = now() WHERE id = $2 AND status = $3`,
		to, id, from)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *paymentIntentRepo) ListByMerchant(ctx context.Context, merchantID string, limit, offset int) ([]domain.PaymentIntent, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, merchant_id, amount_minor, currency, status, method,
			customer_ref, customer_phone, customer_email, idempotency_key, metadata,
			expires_at, created_at, updated_at
		FROM payment_intents WHERE merchant_id = $1
		ORDER BY created_at DESC LIMIT $2 OFFSET $3`, merchantID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pis []domain.PaymentIntent
	for rows.Next() {
		var pi domain.PaymentIntent
		if err := rows.Scan(&pi.ID, &pi.MerchantID, &pi.AmountMinor, &pi.Currency, &pi.Status,
			&pi.Method, &pi.CustomerRef, &pi.CustomerPhone, &pi.CustomerEmail,
			&pi.IdempotencyKey, &pi.Metadata, &pi.ExpiresAt, &pi.CreatedAt, &pi.UpdatedAt); err != nil {
			return nil, err
		}
		pis = append(pis, pi)
	}
	return pis, nil
}
