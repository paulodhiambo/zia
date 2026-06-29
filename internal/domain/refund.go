package domain

import "time"

type RefundStatus string

const (
	RefundPending   RefundStatus = "pending"
	RefundSucceeded RefundStatus = "succeeded"
	RefundFailed    RefundStatus = "failed"
)

type Refund struct {
	ID              string       `db:"id" json:"id"`
	PaymentIntentID string       `db:"payment_intent_id" json:"paymentIntentId"`
	AttemptID       *string      `db:"attempt_id" json:"attemptId"`
	Amount     int64        `db:"amount" json:"amountMinor"`
	Currency        string       `db:"currency" json:"currency"`
	Status          RefundStatus `db:"status" json:"status"`
	Reason          *string      `db:"reason" json:"reason"`
	PSPReference    *string      `db:"psp_reference" json:"pspReference"`
	CreatedAt       time.Time    `db:"created_at" json:"createdAt"`
	UpdatedAt       time.Time    `db:"updated_at" json:"updatedAt"`
}
