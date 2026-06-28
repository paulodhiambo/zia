package domain

import "time"

type PaymentIntentStatus string

const (
	PICreated           PaymentIntentStatus = "created"
	PIRequiresAction    PaymentIntentStatus = "requires_action"
	PIProcessing        PaymentIntentStatus = "processing"
	PISucceeded         PaymentIntentStatus = "succeeded"
	PIFailed            PaymentIntentStatus = "failed"
	PIExpired           PaymentIntentStatus = "expired"
	PIPartiallyRefunded PaymentIntentStatus = "partially_refunded"
	PIRefunded          PaymentIntentStatus = "refunded"
)

type PaymentMethod string

const (
	MethodMpesaSTK PaymentMethod = "mpesa_stk"
	MethodCard     PaymentMethod = "card"
)

type PaymentIntent struct {
	ID             string              `db:"id" json:"id"`
	MerchantID     string              `db:"merchant_id" json:"merchantId"`
	AmountMinor    int64               `db:"amount_minor" json:"amountMinor"`
	Currency       string              `db:"currency" json:"currency"`
	Status         PaymentIntentStatus `db:"status" json:"status"`
	Method         PaymentMethod       `db:"method" json:"method"`
	CustomerRef    string              `db:"customer_ref" json:"customerRef"`
	CustomerPhone  string              `db:"customer_phone" json:"customerPhone"`
	CustomerEmail  string              `db:"customer_email" json:"customerEmail"`
	IdempotencyKey string              `db:"idempotency_key" json:"-"`
	Metadata       []byte              `db:"metadata" json:"metadata,omitempty"`
	ExpiresAt      *time.Time          `db:"expires_at" json:"expiresAt,omitempty"`
	CreatedAt      time.Time           `db:"created_at" json:"createdAt"`
	UpdatedAt      time.Time           `db:"updated_at" json:"updatedAt"`
}
