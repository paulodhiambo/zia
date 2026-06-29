package domain

import "time"

type LedgerEntry struct {
	ID            string    `db:"id" json:"id"`
	AccountID     string    `db:"account_id" json:"accountId"`
	EntryType     string    `db:"entry_type" json:"entryType"`
	Amount   int64     `db:"amount" json:"amountMinor"`
	Currency      string    `db:"currency" json:"currency"`
	ReferenceType string    `db:"reference_type" json:"referenceType"`
	ReferenceID   string    `db:"reference_id" json:"referenceId"`
	PostedAt      time.Time `db:"posted_at" json:"postedAt"`
}
