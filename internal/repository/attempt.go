package repository

import (
	"context"

	"zia/internal/domain"
)

type AttemptRepository interface {
	Create(ctx context.Context, a *domain.Attempt) error
	GetByID(ctx context.Context, id string) (*domain.Attempt, error)
	GetByPSPReference(ctx context.Context, psp, pspReference string) (*domain.Attempt, error)
	UpdateStatus(ctx context.Context, id string, status domain.AttemptStatus) error
	ListByPaymentIntent(ctx context.Context, paymentIntentID string) ([]domain.Attempt, error)
}

type attemptRepo struct {
	db DBTX
}

func NewAttempt(db DBTX) AttemptRepository {
	return &attemptRepo{db: db}
}

func (r *attemptRepo) Create(ctx context.Context, a *domain.Attempt) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO attempts (id, payment_intent_id, psp, psp_reference, status, sequence_no,
			raw_request, raw_response, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		a.ID, a.PaymentIntentID, a.PSP, a.PSPReference, a.Status, a.SequenceNo,
		a.RawRequest, a.RawResponse, a.CreatedAt, a.UpdatedAt)
	return err
}

func (r *attemptRepo) GetByID(ctx context.Context, id string) (*domain.Attempt, error) {
	a := &domain.Attempt{}
	err := r.db.QueryRow(ctx, `
		SELECT id, payment_intent_id, psp, psp_reference, status, sequence_no,
			raw_request, raw_response, created_at, updated_at
		FROM attempts WHERE id = $1`, id).Scan(
		&a.ID, &a.PaymentIntentID, &a.PSP, &a.PSPReference, &a.Status,
		&a.SequenceNo, &a.RawRequest, &a.RawResponse, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *attemptRepo) GetByPSPReference(ctx context.Context, psp, pspReference string) (*domain.Attempt, error) {
	a := &domain.Attempt{}
	err := r.db.QueryRow(ctx, `
		SELECT id, payment_intent_id, psp, psp_reference, status, sequence_no,
			raw_request, raw_response, created_at, updated_at
		FROM attempts WHERE psp = $1 AND psp_reference = $2`, psp, pspReference).Scan(
		&a.ID, &a.PaymentIntentID, &a.PSP, &a.PSPReference, &a.Status,
		&a.SequenceNo, &a.RawRequest, &a.RawResponse, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *attemptRepo) UpdateStatus(ctx context.Context, id string, status domain.AttemptStatus) error {
	_, err := r.db.Exec(ctx,
		`UPDATE attempts SET status = $1, updated_at = now() WHERE id = $2`,
		status, id)
	return err
}

func (r *attemptRepo) ListByPaymentIntent(ctx context.Context, paymentIntentID string) ([]domain.Attempt, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, payment_intent_id, psp, psp_reference, status, sequence_no,
			raw_request, raw_response, created_at, updated_at
		FROM attempts WHERE payment_intent_id = $1
		ORDER BY sequence_no ASC`, paymentIntentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []domain.Attempt
	for rows.Next() {
		var a domain.Attempt
		if err := rows.Scan(&a.ID, &a.PaymentIntentID, &a.PSP, &a.PSPReference, &a.Status,
			&a.SequenceNo, &a.RawRequest, &a.RawResponse, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		attempts = append(attempts, a)
	}
	return attempts, nil
}
