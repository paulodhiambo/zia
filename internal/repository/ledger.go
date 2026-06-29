package repository

import (
	"context"

	"zia/internal/domain"
)

type LedgerRepository interface {
	InsertEntries(ctx context.Context, entries []domain.LedgerEntry) error
	GetByAccount(ctx context.Context, accountID string, limit, offset int) ([]domain.LedgerEntry, error)
	GetByReference(ctx context.Context, referenceType, referenceID string) ([]domain.LedgerEntry, error)
	Balance(ctx context.Context, accountID string) (int64, error)
}

type ledgerRepo struct {
	db DBTX
}

func NewLedger(db DBTX) LedgerRepository {
	return &ledgerRepo{db: db}
}

func (r *ledgerRepo) InsertEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	for _, e := range entries {
		_, err := r.db.Exec(ctx, `
			INSERT INTO ledger_entries (id, account_id, entry_type, amount, currency,
				reference_type, reference_id, posted_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			e.ID, e.AccountID, e.EntryType, e.Amount, e.Currency,
			e.ReferenceType, e.ReferenceID, e.PostedAt)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *ledgerRepo) GetByAccount(ctx context.Context, accountID string, limit, offset int) ([]domain.LedgerEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, entry_type, amount, currency,
			reference_type, reference_id, posted_at
		FROM ledger_entries WHERE account_id = $1
		ORDER BY posted_at DESC LIMIT $2 OFFSET $3`, accountID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.AccountID, &e.EntryType, &e.Amount, &e.Currency,
			&e.ReferenceType, &e.ReferenceID, &e.PostedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *ledgerRepo) GetByReference(ctx context.Context, referenceType, referenceID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, account_id, entry_type, amount, currency,
			reference_type, reference_id, posted_at
		FROM ledger_entries WHERE reference_type = $1 AND reference_id = $2
		ORDER BY posted_at ASC`, referenceType, referenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(&e.ID, &e.AccountID, &e.EntryType, &e.Amount, &e.Currency,
			&e.ReferenceType, &e.ReferenceID, &e.PostedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

func (r *ledgerRepo) Balance(ctx context.Context, accountID string) (int64, error) {
	var balance int64
	err := r.db.QueryRow(ctx, `
		SELECT COALESCE(SUM(CASE WHEN entry_type = 'credit' THEN amount ELSE -amount END), 0)
		FROM ledger_entries WHERE account_id = $1`, accountID).Scan(&balance)
	return balance, err
}
