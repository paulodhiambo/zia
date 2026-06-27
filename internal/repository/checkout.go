package repository

import (
	"context"

	"zia/internal/domain"
)

type CheckoutRepository interface {
	Create(ctx context.Context, cs *domain.CheckoutSession) error
	GetByToken(ctx context.Context, token string) (*domain.CheckoutSession, error)
	GetByPaymentIntent(ctx context.Context, paymentIntentID string) (*domain.CheckoutSession, error)
}

type checkoutRepo struct {
	db DBTX
}

func NewCheckout(db DBTX) CheckoutRepository {
	return &checkoutRepo{db: db}
}

func (r *checkoutRepo) Create(ctx context.Context, cs *domain.CheckoutSession) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO checkout_sessions (id, payment_intent_id, public_token, ui_config, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		cs.ID, cs.PaymentIntentID, cs.PublicToken, cs.UIConfig, cs.ExpiresAt, cs.CreatedAt)
	return err
}

func (r *checkoutRepo) GetByToken(ctx context.Context, token string) (*domain.CheckoutSession, error) {
	cs := &domain.CheckoutSession{}
	err := r.db.QueryRow(ctx, `
		SELECT id, payment_intent_id, public_token, ui_config, expires_at, created_at
		FROM checkout_sessions WHERE public_token = $1`, token).Scan(
		&cs.ID, &cs.PaymentIntentID, &cs.PublicToken, &cs.UIConfig, &cs.ExpiresAt, &cs.CreatedAt)
	if err != nil {
		return nil, err
	}
	return cs, nil
}

func (r *checkoutRepo) GetByPaymentIntent(ctx context.Context, paymentIntentID string) (*domain.CheckoutSession, error) {
	cs := &domain.CheckoutSession{}
	err := r.db.QueryRow(ctx, `
		SELECT id, payment_intent_id, public_token, ui_config, expires_at, created_at
		FROM checkout_sessions WHERE payment_intent_id = $1`, paymentIntentID).Scan(
		&cs.ID, &cs.PaymentIntentID, &cs.PublicToken, &cs.UIConfig, &cs.ExpiresAt, &cs.CreatedAt)
	if err != nil {
		return nil, err
	}
	return cs, nil
}
