package domain

import "time"

type PayoutStatus string

const (
	PayoutPending   PayoutStatus = "pending"
	PayoutSucceeded PayoutStatus = "succeeded"
	PayoutFailed    PayoutStatus = "failed"
)

type Payout struct {
	ID           string       `db:"id" json:"id"`
	MerchantID   string       `db:"merchant_id" json:"merchantId"`
	AmountMinor  int64        `db:"amount_minor" json:"amountMinor"`
	Currency     string       `db:"currency" json:"currency"`
	Rail         string       `db:"rail" json:"rail"`
	Status       PayoutStatus `db:"status" json:"status"`
	PSPReference *string      `db:"psp_reference" json:"pspReference"`
	CreatedAt    time.Time    `db:"created_at" json:"createdAt"`
	UpdatedAt    time.Time    `db:"updated_at" json:"updatedAt"`
}
