package domain

import "time"

type MerchantStatus string

const (
	MerchantActive   MerchantStatus = "active"
	MerchantInactive MerchantStatus = "inactive"
)

type Merchant struct {
	ID               string         `db:"id" json:"id"`
	LegalName        string         `db:"legal_name" json:"legalName"`
	Country          string         `db:"country" json:"country"`
	DefaultCurrency  string         `db:"default_currency" json:"defaultCurrency"`
	Status           MerchantStatus `db:"status" json:"status"`
	SettlementConfig []byte         `db:"settlement_config" json:"settlementConfig"`
	CreatedAt        time.Time      `db:"created_at" json:"createdAt"`
}

type APIKey struct {
	ID          string    `db:"id" json:"id"`
	MerchantID  string    `db:"merchant_id" json:"merchantId"`
	KeyHash     string    `db:"key_hash" json:"-"`
	KeyPrefix   string    `db:"key_prefix" json:"keyPrefix"`
	Environment string    `db:"environment" json:"environment"`
	CreatedAt   time.Time `db:"created_at" json:"createdAt"`
}
