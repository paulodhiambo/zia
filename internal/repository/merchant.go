package repository

import (
	"context"
	"errors"

	"zia/internal/domain"
)

type MerchantRepository interface {
	Create(ctx context.Context, m *domain.Merchant) error
	GetByID(ctx context.Context, id string) (*domain.Merchant, error)
	ListAll(ctx context.Context) ([]domain.Merchant, error)
	UpdateSettlementConfig(ctx context.Context, id string, config []byte) error
	CreateAPIKey(ctx context.Context, k *domain.APIKey) error
	GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error)
	GetAPIKeyByPrefix(ctx context.Context, prefix string) (*domain.APIKey, error)
}

type merchantRepo struct {
	db DBTX
}

func NewMerchant(db DBTX) MerchantRepository {
	return &merchantRepo{db: db}
}

func (r *merchantRepo) Create(ctx context.Context, m *domain.Merchant) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO merchants (id, legal_name, country, default_currency, status,
			settlement_config, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		m.ID, m.LegalName, m.Country, m.DefaultCurrency, m.Status,
		m.SettlementConfig, m.CreatedAt)
	return err
}

func (r *merchantRepo) GetByID(ctx context.Context, id string) (*domain.Merchant, error) {
	m := &domain.Merchant{}
	err := r.db.QueryRow(ctx, `
		SELECT id, legal_name, country, default_currency, status, settlement_config, created_at
		FROM merchants WHERE id = $1`, id).Scan(
		&m.ID, &m.LegalName, &m.Country, &m.DefaultCurrency, &m.Status,
		&m.SettlementConfig, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (r *merchantRepo) UpdateSettlementConfig(ctx context.Context, id string, config []byte) error {
	_, err := r.db.Exec(ctx,
		`UPDATE merchants SET settlement_config = $1 WHERE id = $2`,
		config, id)
	return err
}

func (r *merchantRepo) ListAll(ctx context.Context) ([]domain.Merchant, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, legal_name, country, default_currency, status, settlement_config, created_at
		FROM merchants ORDER BY legal_name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var merchants []domain.Merchant
	for rows.Next() {
		var m domain.Merchant
		if err := rows.Scan(&m.ID, &m.LegalName, &m.Country, &m.DefaultCurrency,
			&m.Status, &m.SettlementConfig, &m.CreatedAt); err != nil {
			return nil, err
		}
		merchants = append(merchants, m)
	}
	return merchants, nil
}

func (r *merchantRepo) CreateAPIKey(ctx context.Context, k *domain.APIKey) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO api_keys (id, merchant_id, key_hash, key_prefix, environment, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		k.ID, k.MerchantID, k.KeyHash, k.KeyPrefix, k.Environment, k.CreatedAt)
	return err
}

func (r *merchantRepo) GetAPIKeyByHash(ctx context.Context, hash string) (*domain.APIKey, error) {
	if r.db == nil {
		return nil, errors.New("no database connection")
	}
	k := &domain.APIKey{}
	err := r.db.QueryRow(ctx, `
		SELECT id, merchant_id, key_hash, key_prefix, environment, created_at
		FROM api_keys WHERE key_hash = $1`, hash).Scan(
		&k.ID, &k.MerchantID, &k.KeyHash, &k.KeyPrefix, &k.Environment, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}

func (r *merchantRepo) GetAPIKeyByPrefix(ctx context.Context, prefix string) (*domain.APIKey, error) {
	k := &domain.APIKey{}
	err := r.db.QueryRow(ctx, `
		SELECT id, merchant_id, key_hash, key_prefix, environment, created_at
		FROM api_keys WHERE key_prefix = $1`, prefix).Scan(
		&k.ID, &k.MerchantID, &k.KeyHash, &k.KeyPrefix, &k.Environment, &k.CreatedAt)
	if err != nil {
		return nil, err
	}
	return k, nil
}
