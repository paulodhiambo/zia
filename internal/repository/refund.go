package repository

import (
	"context"

	"zia/internal/domain"
)

type RefundRepository interface {
	Create(ctx context.Context, r *domain.Refund) error
	GetByID(ctx context.Context, id string) (*domain.Refund, error)
	UpdateStatus(ctx context.Context, id string, status domain.RefundStatus) error
	ListByPaymentIntent(ctx context.Context, paymentIntentID string) ([]domain.Refund, error)
}

type refundRepo struct {
	db DBTX
}

func NewRefund(db DBTX) RefundRepository {
	return &refundRepo{db: db}
}

func (r *refundRepo) Create(ctx context.Context, refund *domain.Refund) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO refunds (id, payment_intent_id, attempt_id, amount_minor, currency,
			status, reason, psp_reference, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		refund.ID, refund.PaymentIntentID, refund.AttemptID, refund.AmountMinor,
		refund.Currency, refund.Status, refund.Reason, refund.PSPReference,
		refund.CreatedAt, refund.UpdatedAt)
	return err
}

func (r *refundRepo) GetByID(ctx context.Context, id string) (*domain.Refund, error) {
	ref := &domain.Refund{}
	err := r.db.QueryRow(ctx, `
		SELECT id, payment_intent_id, attempt_id, amount_minor, currency, status,
			reason, psp_reference, created_at, updated_at
		FROM refunds WHERE id = $1`, id).Scan(
		&ref.ID, &ref.PaymentIntentID, &ref.AttemptID, &ref.AmountMinor, &ref.Currency,
		&ref.Status, &ref.Reason, &ref.PSPReference, &ref.CreatedAt, &ref.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return ref, nil
}

func (r *refundRepo) UpdateStatus(ctx context.Context, id string, status domain.RefundStatus) error {
	_, err := r.db.Exec(ctx,
		`UPDATE refunds SET status = $1, updated_at = now() WHERE id = $2`,
		status, id)
	return err
}

func (r *refundRepo) ListByPaymentIntent(ctx context.Context, paymentIntentID string) ([]domain.Refund, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, payment_intent_id, attempt_id, amount_minor, currency, status,
			reason, psp_reference, created_at, updated_at
		FROM refunds WHERE payment_intent_id = $1
		ORDER BY created_at DESC`, paymentIntentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refunds []domain.Refund
	for rows.Next() {
		var ref domain.Refund
		if err := rows.Scan(&ref.ID, &ref.PaymentIntentID, &ref.AttemptID, &ref.AmountMinor,
			&ref.Currency, &ref.Status, &ref.Reason, &ref.PSPReference,
			&ref.CreatedAt, &ref.UpdatedAt); err != nil {
			return nil, err
		}
		refunds = append(refunds, ref)
	}
	return refunds, nil
}
