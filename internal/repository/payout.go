package repository

import (
	"context"

	"zia/internal/domain"
)

type PayoutRepository interface {
	Create(ctx context.Context, p *domain.Payout) error
	GetByID(ctx context.Context, id string) (*domain.Payout, error)
	UpdateStatus(ctx context.Context, id string, status domain.PayoutStatus) error
}

type payoutRepo struct {
	db DBTX
}

func NewPayout(db DBTX) PayoutRepository {
	return &payoutRepo{db: db}
}

func (r *payoutRepo) Create(ctx context.Context, p *domain.Payout) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO payouts (id, merchant_id, amount_minor, currency, rail, status, psp_reference,
			created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		p.ID, p.MerchantID, p.AmountMinor, p.Currency, p.Rail, p.Status,
		p.PSPReference, p.CreatedAt, p.UpdatedAt)
	return err
}

func (r *payoutRepo) GetByID(ctx context.Context, id string) (*domain.Payout, error) {
	p := &domain.Payout{}
	err := r.db.QueryRow(ctx, `
		SELECT id, merchant_id, amount_minor, currency, rail, status,
			psp_reference, created_at, updated_at
		FROM payouts WHERE id = $1`, id).Scan(
		&p.ID, &p.MerchantID, &p.AmountMinor, &p.Currency, &p.Rail, &p.Status,
		&p.PSPReference, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (r *payoutRepo) UpdateStatus(ctx context.Context, id string, status domain.PayoutStatus) error {
	_, err := r.db.Exec(ctx,
		`UPDATE payouts SET status = $1, updated_at = now() WHERE id = $2`,
		status, id)
	return err
}
