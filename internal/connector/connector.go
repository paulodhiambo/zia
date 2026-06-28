package connector

import "context"

type Connector interface {
	Name() string
	Capabilities() Capabilities
	InitiateCollection(ctx context.Context, req CollectionRequest) (CollectionResult, error)
	GetStatus(ctx context.Context, pspReference string) (StatusResult, error)
	Refund(ctx context.Context, req RefundRequest) (RefundResult, error)
	InitiatePayout(ctx context.Context, req PayoutRequest) (PayoutResult, error)
	ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (WebhookEvent, error)
}

type Capabilities struct {
	SupportsCollection bool
	SupportsPayout     bool
	SupportsRefund     bool
	SupportedCurrencies []string
	SupportedCountries  []string
	ConfirmationStyle   string
}

type CollectionRequest struct {
	PaymentIntentID string
	AmountMinor     int64
	Currency        string
	Method          string
	CustomerPhone   string
	CustomerEmail   string
	CallbackURL     string
	IdempotencyKey  string
}

type CollectionResult struct {
	PSPReference string
	Status       string
	NextAction   *NextAction
	RawRequest   []byte
	RawResponse  []byte
}

type NextAction struct {
	Type string
	URL  string
}

type StatusResult struct {
	Supported bool
	Status    string
	AmountMinor int64
	Currency    string
}

type RefundRequest struct {
	PaymentIntentID string
	AttemptID       string
	PSPReference    string
	AmountMinor     int64
	Currency        string
	Reason          string
}

type RefundResult struct {
	PSPReference string
	Status       string
}

type PayoutRequest struct {
	MerchantID      string
	AmountMinor     int64
	Currency        string
	TargetCurrency  string
	BankAccountRef  string
	IdempotencyKey  string
}

type PayoutResult struct {
	PSPReference string
	Status       string
}

type WebhookEvent struct {
	PSP              string
	EventType        string
	PSPReference     string
	PSPTransactionID string
	DedupKey         string
	Status           string
	AmountMinor      int64
	Currency         string
	RawPayload       []byte
}
