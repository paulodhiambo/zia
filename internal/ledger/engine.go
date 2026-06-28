package ledger

import (
	"context"
	"fmt"
	"time"

	"zia/internal/domain"
	"zia/internal/repository"

	"github.com/google/uuid"
)

type Engine struct {
	repo repository.LedgerRepository
}

func NewEngine(repo repository.LedgerRepository) *Engine {
	return &Engine{repo: repo}
}

type Posting struct {
	AccountID     string
	EntryType     string
	AmountMinor   int64
}

type PostingGroup struct {
	Entries    []domain.LedgerEntry
	Currency   string
	TotalDebit int64
	TotalCredit int64
}

func (e *Engine) PostCollection(ctx context.Context, merchantID, piID, psp string, amountMinor int64, currency string, feeMinor int64) error {
	now := time.Now().UTC()

	pspAccount := pspClearingAccount(psp)
	merchantAccount := MerchantAvailable(merchantID)
	var entries []domain.LedgerEntry

	entries = append(entries,
		domain.LedgerEntry{
			ID:            uuid.New().String(),
			AccountID:     pspAccount,
			EntryType:     "debit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "attempt",
			ReferenceID:   piID,
			PostedAt:      now,
		},
		domain.LedgerEntry{
			ID:            uuid.New().String(),
			AccountID:     merchantAccount,
			EntryType:     "credit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "attempt",
			ReferenceID:   piID,
			PostedAt:      now,
		},
	)

	if feeMinor > 0 {
		entries = append(entries,
			domain.LedgerEntry{
				ID:            uuid.New().String(),
				AccountID:     merchantAccount,
				EntryType:     "debit",
				AmountMinor:   feeMinor,
				Currency:      currency,
				ReferenceType: "fee",
				ReferenceID:   piID,
				PostedAt:      now,
			},
			domain.LedgerEntry{
				ID:            uuid.New().String(),
				AccountID:     AccountPlatformFees,
				EntryType:     "credit",
				AmountMinor:   feeMinor,
				Currency:      currency,
				ReferenceType: "fee",
				ReferenceID:   piID,
				PostedAt:      now,
			},
		)
	}

	return e.postBalanced(ctx, entries)
}

func (e *Engine) PostRefund(ctx context.Context, merchantID, piID, psp string, amountMinor int64, currency string) error {
	now := time.Now().UTC()

	pspAccount := pspClearingAccount(psp)
	merchantAccount := MerchantAvailable(merchantID)

	entries := []domain.LedgerEntry{
		{
			ID:            uuid.New().String(),
			AccountID:     merchantAccount,
			EntryType:     "debit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "refund",
			ReferenceID:   piID,
			PostedAt:      now,
		},
		{
			ID:            uuid.New().String(),
			AccountID:     pspAccount,
			EntryType:     "credit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "refund",
			ReferenceID:   piID,
			PostedAt:      now,
		},
	}

	return e.postBalanced(ctx, entries)
}

func (e *Engine) PostPayoutInit(ctx context.Context, merchantID, payoutID string, amountMinor int64, currency string) error {
	now := time.Now().UTC()

	entries := []domain.LedgerEntry{
		{
			ID:            uuid.New().String(),
			AccountID:     MerchantAvailable(merchantID),
			EntryType:     "debit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "payout",
			ReferenceID:   payoutID,
			PostedAt:      now,
		},
		{
			ID:            uuid.New().String(),
			AccountID:     MerchantInTransit(merchantID),
			EntryType:     "credit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "payout",
			ReferenceID:   payoutID,
			PostedAt:      now,
		},
	}

	return e.postBalanced(ctx, entries)
}

func (e *Engine) PostPayoutComplete(ctx context.Context, merchantID, payoutID string, amountMinor int64, currency string) error {
	now := time.Now().UTC()

	entries := []domain.LedgerEntry{
		{
			ID:            uuid.New().String(),
			AccountID:     MerchantInTransit(merchantID),
			EntryType:     "debit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "payout",
			ReferenceID:   payoutID,
			PostedAt:      now,
		},
		{
			ID:            uuid.New().String(),
			AccountID:     AccountPlatformOperating,
			EntryType:     "credit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "payout",
			ReferenceID:   payoutID,
			PostedAt:      now,
		},
	}

	return e.postBalanced(ctx, entries)
}

func (e *Engine) PostPayoutReversal(ctx context.Context, merchantID, payoutID string, amountMinor int64, currency string) error {
	now := time.Now().UTC()

	entries := []domain.LedgerEntry{
		{
			ID:            uuid.New().String(),
			AccountID:     MerchantInTransit(merchantID),
			EntryType:     "debit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "payout",
			ReferenceID:   payoutID,
			PostedAt:      now,
		},
		{
			ID:            uuid.New().String(),
			AccountID:     MerchantAvailable(merchantID),
			EntryType:     "credit",
			AmountMinor:   amountMinor,
			Currency:      currency,
			ReferenceType: "payout",
			ReferenceID:   payoutID,
			PostedAt:      now,
		},
	}

	return e.postBalanced(ctx, entries)
}

func (e *Engine) Balance(ctx context.Context, accountID string) (int64, error) {
	return e.repo.Balance(ctx, accountID)
}

func (e *Engine) postBalanced(ctx context.Context, entries []domain.LedgerEntry) error {
	if err := validateBalanced(entries); err != nil {
		return err
	}
	return e.repo.InsertEntries(ctx, entries)
}

func validateBalanced(entries []domain.LedgerEntry) error {
	balances := make(map[string]int64)
	for _, e := range entries {
		key := e.Currency
		if e.EntryType == "debit" {
			balances[key] += e.AmountMinor
		} else {
			balances[key] -= e.AmountMinor
		}
	}
	for currency, net := range balances {
		if net != 0 {
			return fmt.Errorf("unbalanced ledger entries for %s: net=%d", currency, net)
		}
	}
	return nil
}

func pspClearingAccount(psp string) string {
	switch psp {
	case "mpesa":
		return AccountPSPClearingMpesa
	case "paystack":
		return AccountPSPClearingPaystack
	default:
		return "psp_clearing:" + psp
	}
}
