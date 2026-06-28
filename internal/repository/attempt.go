package repository

import (
	"context"
	"time"

	"zia/internal/domain"
)

type AttemptRow struct {
	ID              string               `db:"id"`
	PaymentIntentID string               `db:"payment_intent_id"`
	PSP             string               `db:"psp"`
	PSPReference    string               `db:"psp_reference"`
	Status          domain.AttemptStatus `db:"status"`
	AmountMinor     int64                `db:"amount_minor"`
	Currency        string               `db:"currency"`
	CreatedAt       time.Time            `db:"created_at"`
}

type AttemptRepository interface {
	Create(ctx context.Context, a *domain.Attempt) error
	GetByID(ctx context.Context, id string) (*domain.Attempt, error)
	GetByPSPReference(ctx context.Context, psp, pspReference string) (*domain.Attempt, error)
	UpdateStatus(ctx context.Context, id string, status domain.AttemptStatus) error
	UpdateCallbackPayload(ctx context.Context, id string, payload []byte) error
	UpdatePSPTransactionID(ctx context.Context, id string, transactionID string) error
	ListByPaymentIntent(ctx context.Context, paymentIntentID string) ([]domain.Attempt, error)
	ListByDateRange(ctx context.Context, from, to time.Time) ([]AttemptRow, error)
}

type attemptRepo struct {
	db DBTX
}

func NewAttempt(db DBTX) AttemptRepository {
	return &attemptRepo{db: db}
}

func (r *attemptRepo) Create(ctx context.Context, a *domain.Attempt) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO attempts (id, payment_intent_id, psp, psp_reference, psp_transaction_id,
			status, sequence_no, raw_request, raw_response, request_payload, callback_payload,
			created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		a.ID, a.PaymentIntentID, a.PSP, a.PSPReference, a.PSPTransactionID,
		a.Status, a.SequenceNo,
		a.RawRequest, a.RawResponse, a.RequestPayload, a.CallbackPayload,
		a.CreatedAt, a.UpdatedAt)
	return err
}

func (r *attemptRepo) GetByID(ctx context.Context, id string) (*domain.Attempt, error) {
	a := &domain.Attempt{}
	err := r.db.QueryRow(ctx, `
		SELECT id, payment_intent_id, psp, psp_reference, psp_transaction_id,
			status, sequence_no, raw_request, raw_response, request_payload, callback_payload,
			created_at, updated_at
		FROM attempts WHERE id = $1`, id).Scan(
		&a.ID, &a.PaymentIntentID, &a.PSP, &a.PSPReference, &a.PSPTransactionID,
		&a.Status, &a.SequenceNo, &a.RawRequest, &a.RawResponse, &a.RequestPayload,
		&a.CallbackPayload, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *attemptRepo) GetByPSPReference(ctx context.Context, psp, pspReference string) (*domain.Attempt, error) {
	a := &domain.Attempt{}
	err := r.db.QueryRow(ctx, `
		SELECT id, payment_intent_id, psp, psp_reference, psp_transaction_id,
			status, sequence_no, raw_request, raw_response, request_payload, callback_payload,
			created_at, updated_at
		FROM attempts WHERE psp = $1 AND psp_reference = $2`, psp, pspReference).Scan(
		&a.ID, &a.PaymentIntentID, &a.PSP, &a.PSPReference, &a.PSPTransactionID,
		&a.Status, &a.SequenceNo, &a.RawRequest, &a.RawResponse, &a.RequestPayload,
		&a.CallbackPayload, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

func (r *attemptRepo) UpdateCallbackPayload(ctx context.Context, id string, payload []byte) error {
	_, err := r.db.Exec(ctx,
		`UPDATE attempts SET callback_payload = $1, updated_at = now() WHERE id = $2`,
		payload, id)
	return err
}

func (r *attemptRepo) UpdatePSPTransactionID(ctx context.Context, id string, transactionID string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE attempts SET psp_transaction_id = $1, updated_at = now() WHERE id = $2`,
		transactionID, id)
	return err
}

func (r *attemptRepo) UpdateStatus(ctx context.Context, id string, status domain.AttemptStatus) error {
	_, err := r.db.Exec(ctx,
		`UPDATE attempts SET status = $1, updated_at = now() WHERE id = $2`,
		status, id)
	return err
}

func (r *attemptRepo) ListByPaymentIntent(ctx context.Context, paymentIntentID string) ([]domain.Attempt, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, payment_intent_id, psp, psp_reference, psp_transaction_id,
			status, sequence_no, raw_request, raw_response, request_payload, callback_payload,
			created_at, updated_at
		FROM attempts WHERE payment_intent_id = $1
		ORDER BY sequence_no ASC`, paymentIntentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var attempts []domain.Attempt
	for rows.Next() {
		var a domain.Attempt
		if err := rows.Scan(
			&a.ID, &a.PaymentIntentID, &a.PSP, &a.PSPReference, &a.PSPTransactionID,
			&a.Status, &a.SequenceNo, &a.RawRequest, &a.RawResponse, &a.RequestPayload,
			&a.CallbackPayload, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		attempts = append(attempts, a)
	}
	return attempts, nil
}

func (r *attemptRepo) ListByDateRange(ctx context.Context, from, to time.Time) ([]AttemptRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT a.id, a.payment_intent_id, a.psp, a.psp_reference, a.status,
			pi.amount_minor, pi.currency, a.created_at
		FROM attempts a
		JOIN payment_intents pi ON pi.id = a.payment_intent_id
		WHERE a.created_at >= $1 AND a.created_at < $2
		ORDER BY a.created_at ASC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []AttemptRow
	for rows.Next() {
		var row AttemptRow
		if err := rows.Scan(&row.ID, &row.PaymentIntentID, &row.PSP, &row.PSPReference,
			&row.Status, &row.AmountMinor, &row.Currency, &row.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, nil
}
