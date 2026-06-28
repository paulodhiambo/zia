package domain

import "time"

type AttemptStatus string

const (
	AttemptPending        AttemptStatus = "pending"
	AttemptRequiresAction AttemptStatus = "requires_action"
	AttemptProcessing     AttemptStatus = "processing"
	AttemptSucceeded      AttemptStatus = "succeeded"
	AttemptFailed         AttemptStatus = "failed"
)

type Attempt struct {
	ID               string        `db:"id" json:"id"`
	PaymentIntentID  string        `db:"payment_intent_id" json:"paymentIntentId"`
	PSP              string        `db:"psp" json:"psp"`
	PSPReference     string        `db:"psp_reference" json:"pspReference"`
	PSPTransactionID string        `db:"psp_transaction_id" json:"pspTransactionId"`
	Status           AttemptStatus `db:"status" json:"status"`
	SequenceNo       int           `db:"sequence_no" json:"sequenceNo"`
	RawRequest       []byte        `db:"raw_request" json:"-"`
	RawResponse      []byte        `db:"raw_response" json:"-"`
	RequestPayload   []byte        `db:"request_payload" json:"-"`
	CallbackPayload  []byte        `db:"callback_payload" json:"-"`
	CreatedAt        time.Time     `db:"created_at" json:"createdAt"`
	UpdatedAt        time.Time     `db:"updated_at" json:"updatedAt"`
}
