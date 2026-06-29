package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"zia/internal/domain"
)

type TransactionFilter struct {
	Status   string
	Method   string
	DateFrom *time.Time
	DateTo   *time.Time
	Limit    int
	Offset   int
}

type DailyVolumeRow struct {
	Date       time.Time
	Volume     int64
	Count      int
	Successful int
	Failed     int
}

type PaymentIntentRepository interface {
	Create(ctx context.Context, pi *domain.PaymentIntent) error
	GetByID(ctx context.Context, id string) (*domain.PaymentIntent, error)
	UpdateStatus(ctx context.Context, id string, status domain.PaymentIntentStatus) error
	UpdateStatusSafe(ctx context.Context, id string, from, to domain.PaymentIntentStatus) (bool, error)
	ListByMerchant(ctx context.Context, merchantID string, limit, offset int) ([]domain.PaymentIntent, error)
	ListByMerchantFiltered(ctx context.Context, merchantID string, f TransactionFilter) ([]domain.PaymentIntent, error)
	DailyVolume(ctx context.Context, merchantID string, since time.Time) ([]DailyVolumeRow, error)
}

type paymentIntentRepo struct {
	db DBTX
}

func NewPaymentIntent(db DBTX) PaymentIntentRepository {
	return &paymentIntentRepo{db: db}
}

func (r *paymentIntentRepo) Create(ctx context.Context, pi *domain.PaymentIntent) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO payment_intents (id, merchant_id, amount, currency, status, method,
			customer_ref, customer_phone, customer_email, idempotency_key, metadata, expires_at,
			created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		pi.ID, pi.MerchantID, pi.Amount, pi.Currency, pi.Status, pi.Method,
		pi.CustomerRef, pi.CustomerPhone, pi.CustomerEmail, pi.IdempotencyKey, pi.Metadata,
		pi.ExpiresAt, pi.CreatedAt, pi.UpdatedAt)
	return err
}

func (r *paymentIntentRepo) GetByID(ctx context.Context, id string) (*domain.PaymentIntent, error) {
	pi := &domain.PaymentIntent{}
	err := r.db.QueryRow(ctx, `
		SELECT id, merchant_id, amount, currency, status, method,
			customer_ref, customer_phone, customer_email, idempotency_key, metadata,
			expires_at, created_at, updated_at
		FROM payment_intents WHERE id = $1`, id).Scan(
		&pi.ID, &pi.MerchantID, &pi.Amount, &pi.Currency, &pi.Status, &pi.Method,
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
		SELECT id, merchant_id, amount, currency, status, method,
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
		if err := rows.Scan(&pi.ID, &pi.MerchantID, &pi.Amount, &pi.Currency, &pi.Status,
			&pi.Method, &pi.CustomerRef, &pi.CustomerPhone, &pi.CustomerEmail,
			&pi.IdempotencyKey, &pi.Metadata, &pi.ExpiresAt, &pi.CreatedAt, &pi.UpdatedAt); err != nil {
			return nil, err
		}
		pis = append(pis, pi)
	}
	return pis, nil
}

func (r *paymentIntentRepo) ListByMerchantFiltered(ctx context.Context, merchantID string, f TransactionFilter) ([]domain.PaymentIntent, error) {
	args := []any{merchantID}
	clauses := []string{"merchant_id = $1"}
	idx := 2

	if f.Status != "" {
		clauses = append(clauses, fmt.Sprintf("status = $%d", idx))
		args = append(args, f.Status)
		idx++
	}
	if f.Method != "" {
		clauses = append(clauses, fmt.Sprintf("method = $%d", idx))
		args = append(args, f.Method)
		idx++
	}
	if f.DateFrom != nil {
		clauses = append(clauses, fmt.Sprintf("created_at >= $%d", idx))
		args = append(args, *f.DateFrom)
		idx++
	}
	if f.DateTo != nil {
		clauses = append(clauses, fmt.Sprintf("created_at <= $%d", idx))
		args = append(args, *f.DateTo)
		idx++
	}

	limit := f.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}

	query := fmt.Sprintf(`
		SELECT id, merchant_id, amount, currency, status, method,
			customer_ref, customer_phone, customer_email, idempotency_key, metadata,
			expires_at, created_at, updated_at
		FROM payment_intents WHERE %s
		ORDER BY created_at DESC LIMIT $%d OFFSET $%d`,
		strings.Join(clauses, " AND "), idx, idx+1)
	args = append(args, limit, offset)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pis []domain.PaymentIntent
	for rows.Next() {
		var pi domain.PaymentIntent
		if err := rows.Scan(&pi.ID, &pi.MerchantID, &pi.Amount, &pi.Currency, &pi.Status,
			&pi.Method, &pi.CustomerRef, &pi.CustomerPhone, &pi.CustomerEmail,
			&pi.IdempotencyKey, &pi.Metadata, &pi.ExpiresAt, &pi.CreatedAt, &pi.UpdatedAt); err != nil {
			return nil, err
		}
		pis = append(pis, pi)
	}
	return pis, nil
}

func (r *paymentIntentRepo) DailyVolume(ctx context.Context, merchantID string, since time.Time) ([]DailyVolumeRow, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DATE_TRUNC('day', created_at)::date AS day,
			COALESCE(SUM(amount), 0) AS volume,
			COUNT(*) AS count,
			COUNT(*) FILTER (WHERE status = 'succeeded') AS successful,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed
		FROM payment_intents
		WHERE merchant_id = $1 AND created_at >= $2
		GROUP BY day
		ORDER BY day DESC`, merchantID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rowsOut []DailyVolumeRow
	for rows.Next() {
		var r DailyVolumeRow
		if err := rows.Scan(&r.Date, &r.Volume, &r.Count, &r.Successful, &r.Failed); err != nil {
			return nil, err
		}
		rowsOut = append(rowsOut, r)
	}
	return rowsOut, nil
}
