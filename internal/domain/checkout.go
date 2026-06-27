package domain

import "time"

type CheckoutSession struct {
	ID              string     `db:"id" json:"-"`
	PaymentIntentID string     `db:"payment_intent_id" json:"paymentIntentId"`
	PublicToken     string     `db:"public_token" json:"publicToken"`
	UIConfig        []byte     `db:"ui_config" json:"uiConfig,omitempty"`
	ExpiresAt       time.Time  `db:"expires_at" json:"expiresAt"`
	CreatedAt       time.Time  `db:"created_at" json:"createdAt"`
}
