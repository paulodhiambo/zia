package ledger

import (
	"context"
	"testing"

	"zia/internal/domain"
)

type mockLedgerRepo struct {
	entries []domain.LedgerEntry
}

func (m *mockLedgerRepo) InsertEntries(_ context.Context, entries []domain.LedgerEntry) error {
	m.entries = append(m.entries, entries...)
	return nil
}

func (m *mockLedgerRepo) GetByAccount(_ context.Context, accountID string, limit, offset int) ([]domain.LedgerEntry, error) {
	return nil, nil
}

func (m *mockLedgerRepo) GetByReference(_ context.Context, referenceType, referenceID string) ([]domain.LedgerEntry, error) {
	return nil, nil
}

func (m *mockLedgerRepo) Balance(_ context.Context, accountID string) (int64, error) {
	var balance int64
	for _, e := range m.entries {
		if e.AccountID == accountID {
			if e.EntryType == "credit" {
				balance += e.AmountMinor
			} else {
				balance -= e.AmountMinor
			}
		}
	}
	return balance, nil
}

func TestPostCollection(t *testing.T) {
	repo := &mockLedgerRepo{}
	eng := NewEngine(repo)

	err := eng.PostCollection(context.Background(), "mch_1", "pi_1", "mpesa", 100000, "KES", 5000)
	if err != nil {
		t.Fatalf("PostCollection failed: %v", err)
	}

	if len(repo.entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(repo.entries))
	}

	balance, _ := repo.Balance(context.Background(), "psp_clearing:mpesa")
	if balance != -100000 {
		t.Errorf("expected psp_clearing balance -100000, got %d", balance)
	}

	balance, _ = repo.Balance(context.Background(), "platform:fees")
	if balance != 5000 {
		t.Errorf("expected platform:fees balance 5000, got %d", balance)
	}

	merchantBal, _ := repo.Balance(context.Background(), "merchant:mch_1:available")
	if merchantBal != 95000 {
		t.Errorf("expected merchant balance 95000, got %d", merchantBal)
	}

	var totalDebit, totalCredit int64
	for _, e := range repo.entries {
		if e.EntryType == "debit" {
			totalDebit += e.AmountMinor
		} else {
			totalCredit += e.AmountMinor
		}
	}
	if totalDebit != totalCredit {
		t.Errorf("unbalanced: debit=%d credit=%d", totalDebit, totalCredit)
	}
}

func TestPostRefund(t *testing.T) {
	repo := &mockLedgerRepo{}
	eng := NewEngine(repo)

	err := eng.PostRefund(context.Background(), "mch_1", "pi_1", "mpesa", 100000, "KES")
	if err != nil {
		t.Fatalf("PostRefund failed: %v", err)
	}

	if len(repo.entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(repo.entries))
	}

	var totalDebit, totalCredit int64
	for _, e := range repo.entries {
		if e.EntryType == "debit" {
			totalDebit += e.AmountMinor
		} else {
			totalCredit += e.AmountMinor
		}
	}
	if totalDebit != totalCredit {
		t.Errorf("unbalanced: debit=%d credit=%d", totalDebit, totalCredit)
	}
}

func TestValidateBalanced(t *testing.T) {
	tests := []struct {
		name    string
		entries []domain.LedgerEntry
		wantErr bool
	}{
		{
			name: "balanced pair",
			entries: []domain.LedgerEntry{
				{AccountID: "a", EntryType: "debit", AmountMinor: 1000, Currency: "KES"},
				{AccountID: "b", EntryType: "credit", AmountMinor: 1000, Currency: "KES"},
			},
			wantErr: false,
		},
		{
			name: "unbalanced",
			entries: []domain.LedgerEntry{
				{AccountID: "a", EntryType: "debit", AmountMinor: 1000, Currency: "KES"},
				{AccountID: "b", EntryType: "credit", AmountMinor: 500, Currency: "KES"},
			},
			wantErr: true,
		},
		{
			name: "zero-sum across currencies",
			entries: []domain.LedgerEntry{
				{AccountID: "a", EntryType: "debit", AmountMinor: 1000, Currency: "KES"},
				{AccountID: "b", EntryType: "credit", AmountMinor: 1000, Currency: "KES"},
				{AccountID: "c", EntryType: "debit", AmountMinor: 500, Currency: "USD"},
				{AccountID: "d", EntryType: "credit", AmountMinor: 500, Currency: "USD"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBalanced(tt.entries)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateBalanced() error = %v, wantErr = %v", err, tt.wantErr)
			}
		})
	}
}
