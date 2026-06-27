# Zia — Implementation Architecture

**Status:** Draft
**Stack:** Go 1.26, chi, PostgreSQL, Redis, NATS JetStream
**Modular monolith** — separate Go packages with clean interfaces; microservice-ready via strangler fig.
**V1 rails:** M-Pesa (Daraja), KCB (Buni), Paystack, Pesalink. Stripe and PayPal are connector stubs deferred to P5+.

---

## 1. Project Layout

```
.
├── cmd/
│   ├── api/                  # HTTP server entrypoint
│   │   └── main.go
│   ├── worker/               # NATS consumer entrypoint (webhooks, notifications)
│   │   └── main.go
│   └── cron/                 # Scheduled jobs (reconciliation, settlement, token refresh)
│       └── main.go
├── internal/
│   ├── domain/               # Core types, no external dependencies
│   │   ├── paymentintent.go
│   │   ├── attempt.go
│   │   ├── ledger.go
│   │   ├── refund.go
│   │   ├── payout.go
│   │   ├── checkout.go
│   │   ├── merchant.go
│   │   ├── webhookevent.go
│   │   └── errors.go
│   ├── repository/           # Data access layer (Postgres)
│   │   ├── paymentintent.go
│   │   ├── attempt.go
│   │   ├── ledger.go
│   │   ├── refund.go
│   │   ├── payout.go
│   │   ├── checkout.go
│   │   ├── merchant.go
│   │   ├── webhookevent.go
│   │   └── db.go             # pool, tx helpers
│   ├── service/
│   │   ├── paymentintent.go  # orchestrator calls here
│   │   ├── refund.go
│   │   ├── payout.go
│   │   └── checkout.go
│   ├── orchestrator/         # State machine — the Switch
│   │   ├── engine.go
│   │   ├── engine_test.go
│   │   └── state.go
│   ├── routing/              # Routing engine + config store
│   │   ├── engine.go
│   │   ├── rules.go
│   │   └── circuitbreaker.go
│   ├── connector/            # Connector interface + registry
│   │   ├── connector.go      # Connector interface
│   │   ├── registry.go       # Registry of named connectors
│   │   ├── mpesa/
│   │   │   ├── connector.go
│   │   │   ├── auth.go
│   │   │   ├── client.go
│   │   │   └── webhook.go
│   │   ├── kcb/
│   │   │   ├── connector.go
│   │   │   ├── auth.go
│   │   │   ├── client.go
│   │   │   └── webhook.go
│   │   ├── paystack/
│   │   │   ├── connector.go
│   │   │   ├── client.go
│   │   │   └── webhook.go
│   │   └── pesalink/
│   │       ├── connector.go
│   │       ├── auth.go
│   │       ├── client.go
│   │       └── webhook.go
│   ├── webhook/              # Webhook ingestion
│   │   ├── handler.go        # HTTP handler
│   │   ├── dedup.go
│   │   └── processor.go
│   ├── ledger/               # Double-entry posting
│   │   ├── engine.go
│   │   ├── accounts.go
│   │   └── engine_test.go
│   ├── reconciliation/       # Nightly reconciliation jobs
│   │   └── runner.go
│   ├── settlement/           # T+1 settlement runner
│   │   └── runner.go
│   ├── notification/         # Merchant webhook dispatcher
│   │   └── dispatcher.go
│   ├── risk/                 # Rules-based fraud
│   │   ├── engine.go
│   │   └── rules.go
│   ├── idempotency/          # Idempotency key store (Redis)
│   │   └── store.go
│   ├── authn/                # Merchant API key auth + HMAC
│   │   ├── middleware.go
│   │   └── key.go
│   ├── checkout/             # Checkout session service
│   │   └── service.go
│   ├── merchant/             # Merchant + PSP account management
│   │   └── service.go
│   └── api/                  # HTTP handlers (chi router)
│       ├── router.go         # Mount all routes
│       ├── middleware.go     # Request ID, logging, recovery, auth
│       ├── paymentintent.go
│       ├── refund.go
│       ├── checkout.go
│       ├── webhook.go
│       ├── merchant.go
│       └── response.go       # Envelope helpers
├── pkg/
│   ├── httpsign/             # HMAC request signing
│   │   ├── sign.go
│   │   └── verify.go
│   ├── moneyutil/            # Minor-unit arithmetic
│   │   └── money.go
│   └── phoneutil/            # E.164 phone normalisation (shared by M-Pesa + KCB)
│       └── normalize.go
├── migrations/               # golang-migrate SQL files
│   ├── 000001_init.up.sql
│   └── 000001_init.down.sql
├── test/
│   ├── contract/             # Per-PSP sandbox tests
│   └── e2e/
├── deploy/
│   ├── docker/
│   └── k8s/
├── go.mod
├── Dockerfile
├── Makefile
├── docker-compose.yml
├── .env.example
├── architecture.md
└── frontend_spec.yaml
```

---

## 2. Dependency Injection & App Bootstrap

A single `main()` builds the dependency graph. No framework DI container — wire it by hand in `cmd/api/main.go`.

```go
// cmd/api/main.go — simplified
func main() {
    cfg := config.Load()
    db  := postgres.Open(cfg.DatabaseURL)
    rdb := redis.Open(cfg.RedisURL)
    js  := nats.Connect(cfg.NATSURL).JetStream()

    // Repos
    piRepo    := repository.NewPaymentIntent(db)
    attRepo   := repository.NewAttempt(db)
    ledRepo   := repository.NewLedger(db)
    whRepo    := repository.NewWebhookEvent(db)
    merchRepo := repository.NewMerchant(db)

    // Core services
    idempotency := idempotency.NewStore(rdb)
    ledgerEng   := ledger.NewEngine(ledRepo)
    riskEng     := risk.NewEngine(cfg.RiskRules)
    routingEng  := routing.NewEngine(routing.NewConfigStore(db))
    webhookProc := webhook.NewProcessor(whRepo, js)
    notif       := notification.NewDispatcher(js, merchRepo)

    // V1 connector registry — M-Pesa, KCB, Paystack, Pesalink
    // Stripe and PayPal are deferred to P5+.
    registry := connector.NewRegistry()
    registry.Register("mpesa",    mpesa.New(cfg.MPesa))
    registry.Register("kcb",      kcb.New(cfg.KCB))
    registry.Register("paystack", paystack.New(cfg.Paystack))
    registry.Register("pesalink", pesalink.New(cfg.Pesalink))

    // Orchestrator
    orc := orchestrator.New(piRepo, attRepo, registry, routingEng, riskEng, idempotency, ledgerEng, js)

    // API handlers
    router := api.NewRouter(orc, webhookProc, idempotency, cfg.HMACSecret, merchRepo)

    srv := &http.Server{Addr: ":" + cfg.Port, Handler: router}
    // graceful shutdown with signal.Notify
}
```

**Package initialization order:** domain → repository → service → connector → orchestrator → api. No circular imports — the orchestrator depends on interfaces, not concrete connector implementations.

---

## 3. Domain Types

All core types live in `internal/domain/` with zero external imports. Statuses are typed constants, amounts are `int64` minor units.

```go
// internal/domain/paymentintent.go
type PaymentIntentStatus string

const (
    PICreated           PaymentIntentStatus = "created"
    PIRequiresAction    PaymentIntentStatus = "requires_action"
    PIProcessing        PaymentIntentStatus = "processing"
    PISucceeded         PaymentIntentStatus = "succeeded"
    PIFailed            PaymentIntentStatus = "failed"
    PIExpired           PaymentIntentStatus = "expired"
    PIPartiallyRefunded PaymentIntentStatus = "partially_refunded"
    PIRefunded          PaymentIntentStatus = "refunded"
)

// PaymentMethod is the customer-facing collection method.
// Pesalink is intentionally absent — it is a payout/settlement rail only.
type PaymentMethod string

const (
    MethodMpesaSTK   PaymentMethod = "mpesa_stk"   // M-Pesa STK Push (Daraja)
    MethodKCBSTK     PaymentMethod = "kcb_stk"      // KCB mobile-money (Buni — bridges Airtel Money/T-Kash/Vooma)
    MethodCard       PaymentMethod = "card"          // Card — routed to Paystack in V1; Stripe in P5+
    // MethodPaypalRedirect deferred to P5+
)

type PaymentIntent struct {
    ID             string              `db:"id"`
    MerchantID     string              `db:"merchant_id"`
    AmountMinor    int64               `db:"amount_minor"`
    Currency       string              `db:"currency"`
    Status         PaymentIntentStatus `db:"status"`
    CustomerRef    string              `db:"customer_ref"`
    CustomerPhone  string              `db:"customer_phone"`  // required for mpesa_stk, kcb_stk
    CustomerEmail  string              `db:"customer_email"`  // required for card
    AllowedMethods json.RawMessage     `db:"allowed_methods"`
    IdempotencyKey string              `db:"idempotency_key"`
    Metadata       json.RawMessage     `db:"metadata"`
    ExpiresAt      time.Time           `db:"expires_at"`
    CreatedAt      time.Time           `db:"created_at"`
    UpdatedAt      time.Time           `db:"updated_at"`
}
```

```go
// internal/domain/attempt.go
type AttemptStatus string

const (
    AttemptPending        AttemptStatus = "pending"
    AttemptRequiresAction AttemptStatus = "requires_action"
    AttemptProcessing     AttemptStatus = "processing"
    AttemptSucceeded      AttemptStatus = "succeeded"
    AttemptFailed         AttemptStatus = "failed"
)

type Attempt struct {
    ID              string          `db:"id"`
    PaymentIntentID string          `db:"payment_intent_id"`
    PSP             string          `db:"psp"`
    PSPReference    string          `db:"psp_reference"` // Daraja CheckoutRequestID, Paystack reference, KCB IPN ref
    Status          AttemptStatus   `db:"status"`
    SequenceNo      int             `db:"sequence_no"`
    RawRequest      json.RawMessage `db:"raw_request"`
    RawResponse     json.RawMessage `db:"raw_response"`
    CreatedAt       time.Time       `db:"created_at"`
    UpdatedAt       time.Time       `db:"updated_at"`
}
```

```go
// internal/domain/ledger.go
type LedgerEntry struct {
    ID            string    `db:"id"`
    AccountID     string    `db:"account_id"`
    EntryType     string    `db:"entry_type"`     // "debit" | "credit"
    AmountMinor   int64     `db:"amount_minor"`
    Currency      string    `db:"currency"`
    ReferenceType string    `db:"reference_type"` // "attempt" | "refund" | "payout" | "fee"
    ReferenceID   string    `db:"reference_id"`
    PostedAt      time.Time `db:"posted_at"`
}
```

```go
// internal/domain/errors.go
type ErrInsufficientBalance struct{ AccountID string }
type ErrInvalidStateTransition struct{ From, To string }
type ErrConnectorNotAvailable struct{ PSP string }
type ErrIdempotencyConflict struct{ Key string }
```

---

## 4. Repository Layer

One interface + one Postgres implementation per aggregate. Transactions flow through a `context.Context` that carries an optional `pgx.Tx`.

```go
// internal/repository/db.go
type DBTX interface {
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository struct {
    db DBTX
}

func New(db DBTX) *Repository { return &Repository{db: db} }

// WithTx runs fn inside a database transaction.
func (r *Repository) WithTx(ctx context.Context, fn func(context.Context) error) error {
    // begin pgx.Tx, store in context, call fn, commit/rollback
}
```

```go
// internal/repository/paymentintent.go
type PaymentIntentRepository interface {
    Create(ctx context.Context, pi *domain.PaymentIntent) error
    GetByID(ctx context.Context, id string) (*domain.PaymentIntent, error)
    UpdateStatus(ctx context.Context, id string, from, to domain.PaymentIntentStatus) error
}
```

Repository methods receive the business object and map to SQL. No ORM — raw SQL via `pgx` with struct scanning.

---

## 5. Connector Layer

### 5.1 Interface

```go
// internal/connector/connector.go
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
    SupportsCollection  bool
    SupportsPayout      bool
    SupportsRefund      bool
    SupportedCurrencies []string
    SupportedCountries  []string
    // "synchronous" | "webhook_only" | "redirect_then_webhook"
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
}

type NextAction struct {
    Type string // "redirect" | "display_qr" | "poll"
    URL  string
}

type WebhookEvent struct {
    PSP          string
    EventType    string
    PSPReference string
    DedupKey     string // PSP event ID, or hash of payload if PSP doesn't provide one
    Status       string
    AmountMinor  int64
    Currency     string
    RawPayload   []byte
}
```

### 5.2 Registry

```go
type Registry struct {
    mu   sync.RWMutex
    conn map[string]Connector
}

func (r *Registry) Get(name string) (Connector, bool)
func (r *Registry) All() []Connector
func (r *Registry) Register(name string, c Connector)
```

The orchestrator calls `registry.Get(name)` — never imports a connector package directly.

---

### 5.3 M-Pesa Connector (Daraja 3.0)

**Role in V1:** Primary mobile-money collection rail (KES). B2C payout for customer-level refunds.

```go
// internal/connector/mpesa/connector.go
type Config struct {
    ConsumerKey        string
    ConsumerSecret     string
    ShortCode          string
    PassKey            string
    CallbackBase       string
    // B2C — requires a separate Safaricom Go-Live approval
    B2CInitiatorName   string
    B2CSecurityCred    string // RSA-encrypted initiator password (production cert)
    AllowedIPs         []string // Safaricom egress IPs for IP-allowlist verification
}

type Connector struct {
    config Config
    http   *http.Client
    auth   *TokenManager // caches OAuth2 client-credentials token (~55 min TTL)
}

func (c *Connector) Name() string { return "mpesa" }

func (c *Connector) Capabilities() connector.Capabilities {
    return connector.Capabilities{
        SupportsCollection:  true,
        SupportsPayout:      true, // B2C — separate Go-Live required
        SupportsRefund:      true, // routed through B2C
        SupportedCurrencies: []string{"KES"},
        SupportedCountries:  []string{"KE"},
        ConfirmationStyle:   "webhook_only",
    }
}

func (c *Connector) InitiateCollection(ctx context.Context, req connector.CollectionRequest) (connector.CollectionResult, error) {
    // 1. Ensure phone is in 2547XXXXXXXX format (use pkg/phoneutil)
    // 2. Get OAuth token (cached via TokenManager)
    // 3. Build STK Push payload:
    //      BusinessShortCode, Password (base64(ShortCode+PassKey+Timestamp)),
    //      Timestamp, TransactionType="CustomerPayBillOnline",
    //      Amount, PartyA=phone, PartyB=ShortCode,
    //      PhoneNumber=phone, CallBackURL, AccountReference, TransactionDesc
    // 4. POST to /mpesa/stkpush/v1/processrequest
    // 5. On success: return CollectionResult{PSPReference: CheckoutRequestID, Status: "requires_action"}
    // 6. Map Daraja error codes:
    //      400.002.02 = bad request (hard fail)
    //      401.002.01 = auth failure (retryable after token refresh)
    //      500.001.* = Daraja internal (retryable)
}

func (c *Connector) GetStatus(ctx context.Context, pspReference string) (connector.StatusResult, error) {
    // POST to /mpesa/stkpushquery/v1/query with CheckoutRequestID
    // ResultCode 0 = success, 1032 = cancelled, 1037 = timeout, 2001 = wrong PIN
    // Use this as the reconciliation fallback for missed callbacks.
}

func (c *Connector) InitiatePayout(ctx context.Context, req connector.PayoutRequest) (connector.PayoutResult, error) {
    // B2C — POST to /mpesa/b2c/v3/paymentrequest
    // CommandID: "BusinessPayment" (refunds) or "SalaryPayment"
    // Requires B2CInitiatorName + B2CSecurityCred (RSA-encrypted password)
    // Note: B2C requires a *separate* Safaricom production Go-Live approval
    //       from the STK Push Go-Live. Sandbox and production are separate.
}

func (c *Connector) ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (connector.WebhookEvent, error) {
    // 1. Verify source IP against c.config.AllowedIPs — Safaricom does not sign
    //    payloads with HMAC; IP allowlist is the only gate here.
    // 2. Parse STK callback JSON:
    //      Body.stkCallback.ResultCode, ResultDesc,
    //      Body.stkCallback.CallbackMetadata.Item[] (Amount, MpesaReceiptNumber, PhoneNumber)
    // 3. Map ResultCode:
    //      0     → succeeded
    //      1     → failed (insufficient funds — hard, don't retry)
    //      1032  → failed (user cancelled — hard)
    //      1037  → failed (timeout — retryable)
    //      2001  → failed (wrong PIN — hard)
    // 4. DedupKey = MerchantRequestID + ":" + CheckoutRequestID
    // 5. PSPReference = Body.stkCallback.CheckoutRequestID
    // CRITICAL: Always return HTTP 200 to Safaricom regardless of processing outcome.
    //           Safaricom retries aggressively on non-200 or slow responses.
    //           Ack here, process downstream via event bus.
}
```

**Token management:**
```go
// internal/connector/mpesa/auth.go
type TokenManager struct {
    cfg        Config
    mu         sync.Mutex
    token      string
    expiry     time.Time
}

func (t *TokenManager) Token(ctx context.Context) (string, error) {
    t.mu.Lock()
    defer t.mu.Unlock()
    if time.Now().Before(t.expiry.Add(-60 * time.Second)) {
        return t.token, nil
    }
    // GET /oauth/v1/generate?grant_type=client_credentials
    // Basic auth: base64(ConsumerKey:ConsumerSecret)
    // Cache token for expires_in - 60s (typically ~3540s)
}
```

---

### 5.4 KCB Connector (Buni)

**Role in V1:** Mobile-money collection failover for M-Pesa; multi-network bridge reaching Airtel Money, T-Kash, and Vooma via a single integration.

```go
// internal/connector/kcb/connector.go
type Config struct {
    ConsumerKey    string
    ConsumerSecret string
    OrgShortCode   string
    CallbackURL    string // must be registered with KCB (manual/email in sandbox)
    AllowedIPs     []string
    Sandbox        bool
}

func (c *Connector) Name() string { return "kcb" }

func (c *Connector) Capabilities() connector.Capabilities {
    return connector.Capabilities{
        SupportsCollection:  true,
        SupportsPayout:      false, // KCB B2B/B2C payout deferred to P5+
        SupportsRefund:      false, // refunds route back via M-Pesa B2C in V1
        SupportedCurrencies: []string{"KES"},
        SupportedCountries:  []string{"KE"},
        ConfirmationStyle:   "webhook_only",
    }
}

func (c *Connector) InitiateCollection(ctx context.Context, req connector.CollectionRequest) (connector.CollectionResult, error) {
    // 1. Ensure phone is in 2547XXXXXXXX / 2541XXXXXXXX format (use pkg/phoneutil)
    //    KCB Buni accepts: Safaricom (2547*), Airtel (2541*), Telkom T-Kash (2577*)
    // 2. Get OAuth token from sandbox.buni.kcbgroup.com/oauth/token
    //    (grant_type=client_credentials, Basic auth)
    // 3. POST to /api/v1/mpesa/express/stk — KCB's wrapper around M-Pesa Express
    // 4. Return CollectionResult{PSPReference: <KCB request ID>, Status: "requires_action"}
}

func (c *Connector) GetStatus(ctx context.Context, pspReference string) (connector.StatusResult, error) {
    // KCB IPN is push-only — there is no KCB-side poll endpoint for IPN status.
    // For reconciliation, use the transaction status query if KCB exposes one,
    // otherwise mark as "poll_not_supported" and rely solely on IPN + Daraja
    // Transaction Status as the reconciliation path for dual-rail payments.
    // TODO: confirm with KCB Buni technical team whether a status-query API exists.
    return connector.StatusResult{Supported: false}, nil
}

func (c *Connector) ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (connector.WebhookEvent, error) {
    // 1. Verify source IP against c.config.AllowedIPs
    //    KCB IPN callback does not include an HMAC signature in V1 sandbox.
    //    Confirm with KCB if a shared secret/token is added in production.
    // 2. Parse IPN body — KCB sends a result similar to Daraja's STK callback
    //    (ResultCode, ResultDesc, TransactionID, MobileNumber, Amount)
    // 3. Map ResultCode to canonical status (same mapping as M-Pesa where applicable)
    // 4. DedupKey = KCB transaction ID + ":" + MobileNumber
    // IMPORTANT: Sandbox IPN callback URL registration is manual (email KCB Buni team).
    //            Document the production callback URL in config before go-live.
}
```

**KCB sandbox note:** The KCB Buni sandbox (`sandbox.buni.kcbgroup.com`) requires manual IPN URL registration by emailing the KCB Buni technical team. Provision `MPESA_KCB_CALLBACK_URL` as a config value from day one — never hardcode it.

---

### 5.5 Paystack Connector

**Role in V1:** Card collection (primary card rail for KES, NGN, GHS, and other supported African currencies). Also supports Paystack-native mobile money in Ghana/Uganda.

```go
// internal/connector/paystack/connector.go
type Config struct {
    SecretKey     string // "sk_test_..." or "sk_live_..." — static key, no token refresh needed
    WebhookSecret string // used to verify x-paystack-signature
}

func (c *Connector) Name() string { return "paystack" }

func (c *Connector) Capabilities() connector.Capabilities {
    return connector.Capabilities{
        SupportsCollection:  true,
        SupportsPayout:      false, // Paystack Transfers API deferred to P5+
        SupportsRefund:      true,
        SupportedCurrencies: []string{"KES", "NGN", "GHS", "ZAR", "USD"},
        SupportedCountries:  []string{"KE", "NG", "GH", "ZA"},
        ConfirmationStyle:   "redirect_then_webhook",
    }
}

func (c *Connector) InitiateCollection(ctx context.Context, req connector.CollectionRequest) (connector.CollectionResult, error) {
    // 1. POST to https://api.paystack.co/transaction/initialize
    //    Headers: Authorization: Bearer <SecretKey>
    //    Body: { amount (in kobo/cents), email, reference (our IdempotencyKey),
    //            currency, callback_url, metadata }
    // 2. On success: response contains { authorization_url, access_code, reference }
    // 3. Return CollectionResult{
    //        PSPReference: reference,
    //        Status: "requires_action",
    //        NextAction: &NextAction{Type: "redirect", URL: authorization_url},
    //    }
    // NOTE: For inline popup (widget), use access_code with Paystack.js instead of redirect.
    //       The redirect flow is used for server-to-server V1; the widget will use inline.
}

func (c *Connector) GetStatus(ctx context.Context, pspReference string) (connector.StatusResult, error) {
    // GET https://api.paystack.co/transaction/verify/:reference
    // Use as reconciliation fallback when charge.success webhook is missed.
    // data.status: "success" | "failed" | "abandoned" | "ongoing" | "pending"
    // Map "success" → succeeded, "failed"/"abandoned" → failed, others → processing
}

func (c *Connector) Refund(ctx context.Context, req connector.RefundRequest) (connector.RefundResult, error) {
    // POST https://api.paystack.co/refund
    // Body: { transaction: pspReference, amount (optional — omit for full refund) }
    // Paystack refunds are asynchronous; status confirmed via refund.processed webhook.
}

func (c *Connector) ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (connector.WebhookEvent, error) {
    // 1. Verify x-paystack-signature header:
    //    HMAC-SHA512(body, WebhookSecret) — must match header value
    //    Reject (401) if mismatch.
    // 2. Parse event JSON — key fields: event, data.reference, data.status,
    //    data.amount, data.currency, data.id
    // 3. Relevant event types:
    //    "charge.success"    → succeeded
    //    "charge.failed"     → failed
    //    "refund.processed"  → refund confirmed
    //    "refund.failed"     → refund failed
    // 4. DedupKey = data.id (Paystack's own unique event ID)
    // 5. PSPReference = data.reference (our reference passed at initialization)
}
```

**No token refresh needed:** Paystack uses a static secret key (`sk_test_*` / `sk_live_*`). Unlike M-Pesa and KCB, there is no OAuth token to manage or refresh. The cron token-refresh job does NOT apply to this connector.

---

### 5.6 Pesalink Connector

**Role in V1:** Merchant settlement and cross-currency payouts. **Not a collection rail** — this connector is only invoked by the Settlement & Payout Service, never by the Orchestrator's `InitiateCollection` path.

The Pesalink flow is a four-step chain: **Quote → Recipient → Transfer → Fund**. Each step is a separate API call; the transfer is asynchronous (confirmed via webhook or polling).

```go
// internal/connector/pesalink/connector.go
type Config struct {
    APIToken   string // JWT or personal/business API token
    ProfileID  string // Pesalink profile/account ID
    BaseURL    string // "https://api.transferwise.com" in production
    Sandbox    bool
}

func (c *Connector) Name() string { return "pesalink" }

func (c *Connector) Capabilities() connector.Capabilities {
    return connector.Capabilities{
        SupportsCollection:  false, // Pesalink is payout-only
        SupportsPayout:      true,
        SupportsRefund:      false,
        SupportedCurrencies: []string{"KES", "USD", "GBP", "EUR", "NGN"},
        ConfirmationStyle:   "webhook_only",
    }
}

// InitiateCollection is a no-op stub — the routing engine must never route
// a collection request to Pesalink. Calling this is a programming error.
func (c *Connector) InitiateCollection(_ context.Context, _ connector.CollectionRequest) (connector.CollectionResult, error) {
    return connector.CollectionResult{}, fmt.Errorf("pesalink: collection not supported")
}

func (c *Connector) InitiatePayout(ctx context.Context, req connector.PayoutRequest) (connector.PayoutResult, error) {
    // Step 1 — Quote
    //   POST /v3/profiles/{profileID}/quotes
    //   Body: { sourceCurrency, targetCurrency, sourceAmount or targetAmount }
    //   Returns: { id: quoteID, rate, fee, ... }
    //
    // Step 2 — Recipient (create or reuse cached recipient ID)
    //   POST /v1/accounts
    //   Body: { currency, type, profile, accountHolderName, details: { ... } }
    //   Cache recipientID by (merchantID, currency) to avoid recreating on every payout.
    //
    // Step 3 — Transfer
    //   POST /v1/transfers
    //   Body: { targetAccount: recipientID, quoteUuid: quoteID,
    //           customerTransactionId: req.IdempotencyKey }
    //   Returns: { id: transferID, status: "incoming_payment_waiting" }
    //
    // Step 4 — Fund
    //   POST /v3/profiles/{profileID}/transfers/{transferID}/payments
    //   Body: { type: "BALANCE" }
    //   Returns: { status: "COMPLETED" | "REJECTED" | "CANCELLED" }
    //
    // Return PayoutResult{PSPReference: transferID, Status: "processing"}
    // Terminal status arrives via transfer-state webhook.
}

func (c *Connector) ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (connector.WebhookEvent, error) {
    // 1. Verify webhook signature — Pesalink signs with RSA public key.
    //    Header: X-Signature-SHA256 (base64 RSA signature over payload)
    //    Verify against Pesalink's published public key.
    // 2. Parse event_type:
    //    "transfers#state-change" with current_state:
    //       "outgoing_payment_sent" → payout succeeded
    //       "funds_refunded"        → payout failed, funds returned
    //       "cancelled"             → payout cancelled
    // 3. DedupKey = resource.id + ":" + current_state
    // 4. PSPReference = resource.id (transferID)
}
```

**Recipient caching:** Pesalink charges a small fee per new recipient account. Cache recipient IDs in Redis keyed by `(merchantID, currency, accountDetails_hash)` to avoid re-creating recipients on every settlement run. Expire cache entries after 30 days to allow for account detail changes.

---

## 6. Orchestrator

The orchestrator is a stateless state machine executor. It receives a command (e.g., `CreatePaymentIntent`), consults the routing engine, invokes the selected connector, persists the attempt, and transitions the payment intent state.

```go
// internal/orchestrator/engine.go
type Engine struct {
    piRepo      repository.PaymentIntentRepository
    attRepo     repository.AttemptRepository
    registry    *connector.Registry
    router      routing.Router
    risk        risk.Engine
    idempotency idempotency.Store
    ledger      *ledger.Engine
    events      nats.JetStreamContext
}

func (e *Engine) CreatePaymentIntent(ctx context.Context, req CreatePIRequest) (*domain.PaymentIntent, error) {
    // 1. Idempotency check
    // 2. Risk check
    // 3. Route: router.Route(ctx, req) — returns primary + fallback connectors
    // 4. Get connector from registry
    // 5. Call connector.InitiateCollection
    // 6. Save attempt (sequence_no=1)
    // 7. Transition PI state (created → requires_action or processing)
    // 8. Publish zia.payment.created event
}

func (e *Engine) HandleWebhookEvent(ctx context.Context, event domain.WebhookEvent) error {
    // Called by the worker consumer after event bus delivery
    // 1. Load Attempt by PSPReference
    // 2. Validate state transition via IsValidTransition
    // 3. If succeeded: post balanced ledger entries in same DB transaction as status update
    // 4. Transition PI state
    // 5. Publish zia.payment.succeeded or zia.payment.failed
}
```

### 6.1 Failover Logic

If `Attempt[n]` fails for a *retryable* reason (PSP timeout, 5xx, circuit-breaker open, insufficient PSP float), the Orchestrator asks the Routing Engine for the next candidate connector and creates `Attempt[n+1]` against the **same** `PaymentIntent`.

Hard failures are **not** retried: insufficient funds, user-cancelled PIN entry (M-Pesa ResultCode 1032), incorrect PIN (ResultCode 2001), card declined as fraud. These surface to the merchant as `failed` immediately.

### 6.2 State Machine

Transitions are table-driven:

```go
// internal/orchestrator/state.go
var transitions = map[domain.PaymentIntentStatus][]domain.PaymentIntentStatus{
    domain.PICreated:        {domain.PIRequiresAction, domain.PIProcessing, domain.PIFailed, domain.PIExpired},
    domain.PIRequiresAction: {domain.PIProcessing, domain.PIFailed, domain.PIExpired},
    domain.PIProcessing:     {domain.PISucceeded, domain.PIFailed, domain.PIRequiresAction},
    domain.PISucceeded:      {domain.PIPartiallyRefunded, domain.PIRefunded},
    // terminal: PIFailed, PIExpired, PIPartiallyRefunded, PIRefunded — no outbound transitions
}

func IsValidTransition(from, to domain.PaymentIntentStatus) bool {
    for _, allowed := range transitions[from] {
        if allowed == to { return true }
    }
    return false
}
```

### 6.3 Event Publication

State transitions publish NATS JetStream messages on subject `zia.payment.<event_type>`:

| Event | Subject | Payload |
|---|---|---|
| Payment created | `zia.payment.created` | `{id, merchant_id, amount, currency, status}` |
| Payment succeeded | `zia.payment.succeeded` | `{id, attempt_id, psp, psp_reference}` |
| Payment failed | `zia.payment.failed` | `{id, attempt_id, psp, reason}` |
| Refund issued | `zia.payment.refunded` | `{id, refund_id, amount}` |

Consumers: `worker` (notification dispatch), `settlement` (trigger on terminal state), `audit` (log sink).

---

## 7. Routing Engine

```go
// internal/routing/engine.go
type Engine struct {
    store RuleStore
    cb    *CircuitBreaker
}

func (e *Engine) Route(ctx context.Context, req RouteRequest) (*RouteDecision, error) {
    // 1. Load rules from store (cached in-memory, refreshed every 60s)
    // 2. Evaluate rules in priority order
    // 3. For each candidate, check circuit breaker
    // 4. Return primary + fallbacks
}
```

```go
type Rule struct {
    Priority   int       `json:"priority"`
    Conditions Condition `json:"conditions"`
    PrimaryPSP string    `json:"primary_psp"`
    Fallbacks  []string  `json:"fallbacks"`
}

type Condition struct {
    Currency  string `json:"currency,omitempty"`
    Method    string `json:"method,omitempty"`
    Country   string `json:"country,omitempty"`
    Merchant  string `json:"merchant_id,omitempty"` // explicit override
}
```

### 7.1 V1 Routing Rules (stored as data, not code)

| Priority | Condition | Primary | Fallback | Notes |
|---|---|---|---|---|
| 1 | Merchant has explicit PSP override | configured PSP | — | Per-merchant routing override |
| 2 | `currency=KES` + `method=mpesa_stk` | `mpesa` | `kcb` | KCB as M-Pesa failover |
| 3 | `currency=KES` + `method=kcb_stk` | `kcb` | `mpesa` | Explicit KCB request; M-Pesa as fallback |
| 4 | `currency=KES` + `method=card` | `paystack` | — | Paystack is sole card rail in V1 |
| 5 | Settlement/payout (internal) | `pesalink` | — | Never exposed to collection routing |
| 6 | Default catch-all | highest-priority connector listing the currency in `Capabilities.SupportedCurrencies` | — | Safety net |

**Important routing constraint:** The routing engine must explicitly exclude `pesalink` from any collection routing decision. Add an allowlist check: `if psp == "pesalink" { skip }` when iterating candidates for collection requests.

### 7.2 Circuit Breaker

```go
type CircuitBreaker struct {
    thresholds map[string]BreakerConfig
    state      sync.Map
}

type BreakerState struct {
    Failures    int
    LastFailure time.Time
    Open        bool
    Cooldown    time.Duration
}

func (cb *CircuitBreaker) RecordFailure(psp string)
func (cb *CircuitBreaker) RecordSuccess(psp string)
func (cb *CircuitBreaker) IsAvailable(psp string) bool
```

Default: open after 5 consecutive failures, 60s cooldown. Per-PSP thresholds can be tuned — M-Pesa sandbox is less stable than Paystack, so consider a higher failure threshold for M-Pesa in non-production environments. Emits `circuit_breaker_open` metric on each transition.

---

## 8. Webhook Processing Pipeline

### 8.1 HTTP Handler

```go
// internal/webhook/handler.go
func Handler(registry *connector.Registry, dedup *DedupStore, whRepo repository.WebhookEventRepository, js nats.JetStreamContext) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        psp := chi.URLParam(r, "psp")
        conn, ok := registry.Get(psp)
        if !ok { http.Error(w, "unknown psp", 404); return }

        body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB limit

        // 1. Parse + verify signature via connector (PSP-specific)
        event, err := conn.ParseWebhook(r.Context(), headersToMap(r.Header), body)
        if err != nil { w.WriteHeader(401); return }

        // 2. Dedup check (Redis, 7-day window)
        seen, _ := dedup.Check(r.Context(), event.DedupKey)
        if seen { w.WriteHeader(200); return }

        // 3. Persist (status=received)
        whRepo.Create(r.Context(), &event)

        // 4. Publish to event bus (async processing)
        js.Publish("zia.webhook.received", marshalEvent(event))

        // 5. 200 OK immediately — BEFORE event bus processing
        //    M-Pesa in particular retries aggressively on slow/non-200 responses.
        w.WriteHeader(200)
    }
}
```

### 8.2 Signature Verification Per PSP

| PSP | Verification method |
|---|---|
| **M-Pesa** | IP allowlist (Safaricom publishes egress IPs). No HMAC on STK callbacks. |
| **KCB** | IP allowlist. Confirm with KCB Buni whether a shared secret header is added in production. |
| **Paystack** | `x-paystack-signature` = HMAC-SHA512(raw body, secret key). Reject if mismatch. |
| **Pesalink** | `X-Signature-SHA256` RSA signature (base64-encoded) over raw body. Verify against Pesalink's published public key. |

### 8.3 Consumer (worker process)

```go
// cmd/worker/main.go subscribes to zia.webhook.received
// For each event:
//   1. Load Attempt by PSPReference
//   2. Validate state transition
//   3. Post ledger entries if succeeded (inside DB transaction)
//   4. Update PI + Attempt status
//   5. Publish zia.payment.succeeded / zia.payment.failed
//   6. On processing error: msg.Nak() with backoff; after N retries → DLQ
```

---

## 9. Ledger Engine

```go
// internal/ledger/engine.go
type Engine struct {
    repo repository.LedgerRepository
}

func (e *Engine) PostEntries(ctx context.Context, entries []domain.LedgerEntry) error {
    // Run in a DB transaction
    // 1. Validate all entries balance: sum(debits) == sum(credits) per currency
    // 2. Insert all entries atomically
    // 3. On failure: rollback, log, alert (ledger imbalance = severity-1 page)
}
```

### 9.1 Account Model

| Account ID | Type | Description |
|---|---|---|
| `psp_clearing:mpesa` | Liability | Funds received from M-Pesa, awaiting settlement |
| `psp_clearing:kcb` | Liability | Funds received from KCB, awaiting settlement |
| `psp_clearing:paystack` | Liability | Paystack balance awaiting settlement |
| `merchant:<id>:available` | Liability | Funds available for merchant payout |
| `merchant:<id>:in_transit` | Liability | Funds being settled via Pesalink (awaiting transfer confirmation) |
| `merchant:<id>:settled` | Liability | Funds confirmed settled by Pesalink |
| `platform:fees` | Revenue | Platform fee income |
| `platform:operating` | Asset | Operating account |

### 9.2 Double-Entry Patterns

**Collection via M-Pesa or KCB (net of fee):**
```
debit  psp_clearing:mpesa            100000   # KES 1,000 received
credit merchant:<id>:available        95000   # net to merchant
debit  merchant:<id>:available         5000   # platform fee
credit platform:fees                   5000
```

**Collection via Paystack card:**
```
debit  psp_clearing:paystack         100000
credit merchant:<id>:available        97000   # after Paystack processing fee + platform fee
debit  merchant:<id>:available         3000
credit platform:fees                   3000
```

**Customer refund via M-Pesa B2C:**
```
debit  merchant:<id>:available       100000
credit psp_clearing:mpesa            100000
```

**Merchant payout via Pesalink (initiate):**
```
debit  merchant:<id>:available       100000
credit merchant:<id>:in_transit      100000
```

**Pesalink transfer confirmed (`outgoing_payment_sent` webhook):**
```
debit  merchant:<id>:in_transit      100000
credit merchant:<id>:settled         100000
```

**Pesalink transfer failed (`funds_refunded` webhook):**
```
debit  merchant:<id>:in_transit      100000
credit merchant:<id>:available       100000   # reverse the initiation
```

---

## 10. API Layer

### 10.1 Middleware Chain

```
RequestID → Recoverer → Logger → RateLimit → Auth (if protected) → Idempotency → Handler
```

### 10.2 Response Envelope

Every response follows a consistent envelope (matching the openapi.yaml specification):

```go
func respond(w http.ResponseWriter, r *http.Request, status int, data any) {
    msgID := middleware.GetReqID(r.Context())
    resp := ResponseEnvelope{
        StatusCode:         "0",
        StatusDescription:  "Success",
        MessageCode:        strconv.Itoa(status),
        MessageDescription: http.StatusText(status),
        MessageID:          msgID,
        PrimaryData:        data,
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(resp)
}

func respondError(w http.ResponseWriter, r *http.Request, status int, code, msg string) {
    msgID := middleware.GetReqID(r.Context())
    resp := ResponseEnvelope{
        StatusCode:         code,
        StatusDescription:  "BusinessError",
        MessageCode:        strconv.Itoa(status),
        MessageDescription: msg,
        MessageID:          msgID,
        PrimaryData:        nil,
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(resp)
}
```

All field names in JSON responses use **camelCase** (`amountMinor`, `merchantId`) to match the openapi.yaml envelope convention. Internal Go struct fields and DB column names remain snake_case.

### 10.3 Route Mounting

```go
// internal/api/router.go
func NewRouter(orc, webhookProc, idempotency, ...) http.Handler {
    r := chi.NewRouter()
    r.Use(middleware.RequestID)
    r.Use(middleware.Recoverer)
    r.Use(middleware.Logger)

    // Public (widget-facing)
    r.Get("/v1/checkout_sessions/{token}", checkoutHandler.GetSession)

    // Merchant-authenticated
    r.Group(func(r chi.Router) {
        r.Use(authn.Middleware(merchantRepo))
        r.Use(idempotency.Middleware()) // on POST/PUT

        r.Post("/v1/payment_intents", piHandler.Create)
        r.Get("/v1/payment_intents/{id}", piHandler.Get)
        r.Post("/v1/payment_intents/{id}/confirm", piHandler.Confirm)
        r.Post("/v1/payment_intents/{id}/refunds", refundHandler.Create)
        r.Get("/v1/transactions", piHandler.List)
        r.Post("/v1/checkout_sessions", checkoutHandler.CreateSession)
    })

    // PSP webhooks — no merchant auth, PSP-specific verification inside connector
    r.Post("/v1/webhooks/{psp}", webhookHandler.Ingest)

    return r
}
```

---

## 11. Background Workers (cmd/worker)

```go
// cmd/worker/main.go
func main() {
    js := nats.Connect(cfg.NATSURL).JetStream()
    // durable consumer on "zia.webhook.received"
    sub, _ := js.SubscribeSync("zia.webhook.received")

    for {
        msg, _ := sub.NextMsg(ctx)
        var event domain.WebhookEvent
        json.Unmarshal(msg.Data, &event)
        // process — if error, msg.Nak() with backoff; after N retries, move to DLQ
    }
}
```

Separate consumers for:
- `zia.webhook.received` → orchestrator event processing
- `zia.payment.succeeded` → notification dispatcher
- `zia.payment.failed` → notification dispatcher
- `zia.payment.created` → settlement scheduler (optional)

---

## 12. Scheduled Jobs (cmd/cron)

### 12.1 Reconciliation Runner

```
Every hour: Get batch of Attempts with status=processing older than 30 min.
  M-Pesa:   call connector.GetStatus(pspReference) via Daraja Query API.
  KCB:      no poll endpoint — flag as "manual review" if older than threshold;
             rely on nightly statement reconciliation.
  Paystack: call GET /transaction/verify/:reference.

Every night at 02:00: Full reconciliation per PSP.
  M-Pesa:   pull Daraja Transaction Status / C2B transaction report.
  KCB:      pull KCB Buni settlement report.
  Paystack: call GET /transaction endpoint with date filter.
  Pesalink: pull transfer statement.
  Match against Attempt/Payout records by psp_reference.
  Generate exception report for: amount mismatches, orphaned PSP transactions,
  orphaned local transactions.
```

### 12.2 Settlement Runner

```
Every hour: Query merchants with available balance > settlement threshold.
  For each:
    1. Compute settlement amount per currency.
    2. Create payout record.
    3. Call pesalink.InitiatePayout (Quote → Recipient → Transfer → Fund).
    4. Post ledger entries: merchant:available → merchant:in_transit.
    5. Terminal confirmation arrives via Pesalink transfer-state webhook.
```

### 12.3 Token Refresh Runner

Only M-Pesa and KCB use OAuth2 tokens that expire and require proactive refresh.
Paystack uses a static secret key — no refresh needed.
Pesalink uses a long-lived API token — monitor expiry and alert if approaching.

```
Every 50 min:
  Refresh M-Pesa OAuth token (expires in ~3600s; refresh at ~3540s).
  Refresh KCB Buni OAuth token (same pattern).
  Check Pesalink API token expiry — alert if within 7 days of expiry.
```

---

## 13. Configuration

```go
// loaded from env vars at startup — no config file in production
type Config struct {
    Port        string // default "8080"
    DatabaseURL string
    RedisURL    string
    NATSURL     string
    HMACSecret  string

    // V1 connectors
    MPesa    mpesa.Config
    KCB      kcb.Config
    Paystack paystack.Config
    Pesalink pesalink.Config

    // Deferred to P5+
    // Stripe stripe.Config
    // PayPal paypal.Config

    RiskRules risk.Config
}
```

**Environment variable mapping (`.env.example`):**

```bash
# Core
PORT=8080
DATABASE_URL=postgres://zia:zia@localhost:5432/zia?sslmode=disable
REDIS_URL=redis://localhost:6379/0
NATS_URL=nats://localhost:4222
HMAC_SIGNING_SECRET=

# M-Pesa (Daraja)
MPESA_CONSUMER_KEY=
MPESA_CONSUMER_SECRET=
MPESA_SHORTCODE=
MPESA_PASSKEY=
MPESA_CALLBACK_BASE_URL=
MPESA_ALLOWED_IPS=196.201.214.200,196.201.214.206,196.201.213.114,196.201.214.207,196.201.214.208,196.201.213.44,196.201.212.127,196.201.212.138,196.201.212.129,196.201.212.136,196.201.212.74,196.201.212.69
# B2C (separate Go-Live required)
MPESA_B2C_INITIATOR_NAME=
MPESA_B2C_SECURITY_CREDENTIAL=

# KCB (Buni)
KCB_CONSUMER_KEY=
KCB_CONSUMER_SECRET=
KCB_ORG_SHORTCODE=
KCB_CALLBACK_URL=     # registered manually with KCB Buni team
KCB_ALLOWED_IPS=      # confirm KCB egress IPs with their technical team

# Paystack
PAYSTACK_SECRET_KEY=
PAYSTACK_WEBHOOK_SECRET=

# Pesalink
PESALINK_API_TOKEN=
PESALINK_PROFILE_ID=
PESALINK_BASE_URL=https://api.transferwise.com
```

---

## 14. Database Migrations (Outline)

### 000001_init.up.sql

```sql
CREATE TABLE merchants (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    legal_name       TEXT NOT NULL,
    country          TEXT NOT NULL,
    default_currency TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'active',
    settlement_config JSONB,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id),
    key_hash    TEXT NOT NULL,
    key_prefix  TEXT NOT NULL,  -- first 8 chars for identification
    environment TEXT NOT NULL,  -- 'live' | 'test'
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payment_intents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id     UUID NOT NULL REFERENCES merchants(id),
    amount_minor    BIGINT NOT NULL,
    currency        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'created',
    customer_ref    TEXT,
    customer_phone  TEXT,
    customer_email  TEXT,
    allowed_methods JSONB,
    idempotency_key TEXT,
    metadata        JSONB,
    expires_at      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_pi_merchant ON payment_intents(merchant_id);
CREATE INDEX idx_pi_status ON payment_intents(status);
CREATE UNIQUE INDEX idx_pi_idempotency ON payment_intents(merchant_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE attempts (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_intent_id UUID NOT NULL REFERENCES payment_intents(id),
    psp               TEXT NOT NULL,
    psp_reference     TEXT,
    status            TEXT NOT NULL,
    sequence_no       INT NOT NULL DEFAULT 1,
    raw_request       JSONB,
    raw_response      JSONB,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_attempts_pi ON attempts(payment_intent_id);
CREATE INDEX idx_attempts_psp_ref ON attempts(psp, psp_reference)
    WHERE psp_reference IS NOT NULL;  -- used by webhook handler lookup

CREATE TABLE ledger_entries (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id     TEXT NOT NULL,
    entry_type     TEXT NOT NULL CHECK (entry_type IN ('debit', 'credit')),
    amount_minor   BIGINT NOT NULL CHECK (amount_minor > 0),
    currency       TEXT NOT NULL,
    reference_type TEXT NOT NULL,
    reference_id   UUID NOT NULL,
    posted_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ledger_account ON ledger_entries(account_id);
CREATE INDEX idx_ledger_reference ON ledger_entries(reference_type, reference_id);

CREATE TABLE webhook_events (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    psp               TEXT NOT NULL,
    event_type        TEXT NOT NULL,
    psp_reference     TEXT,
    dedup_key         TEXT NOT NULL,
    payload           JSONB,
    processing_status TEXT NOT NULL DEFAULT 'received',
    received_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_webhook_dedup ON webhook_events(dedup_key);

CREATE TABLE refunds (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_intent_id UUID NOT NULL REFERENCES payment_intents(id),
    attempt_id        UUID REFERENCES attempts(id), -- the attempt being refunded
    amount_minor      BIGINT NOT NULL,
    currency          TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending',
    reason            TEXT,
    psp_reference     TEXT, -- PSP refund ID
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payouts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id  UUID NOT NULL REFERENCES merchants(id),
    amount_minor BIGINT NOT NULL,
    currency     TEXT NOT NULL,
    rail         TEXT NOT NULL,  -- 'pesalink' in V1
    status       TEXT NOT NULL DEFAULT 'pending',
    psp_reference TEXT,          -- Pesalink transferID
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE checkout_sessions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    payment_intent_id UUID NOT NULL REFERENCES payment_intents(id),
    public_token      TEXT NOT NULL UNIQUE,
    ui_config         JSONB,
    expires_at        TIMESTAMPTZ NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE routing_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    priority    INT NOT NULL,
    conditions  JSONB NOT NULL,
    primary_psp TEXT NOT NULL,
    fallbacks   JSONB,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_routing_rules_priority ON routing_rules(priority) WHERE enabled = true;

-- Recipient cache for Pesalink — avoids re-creating recipients on every settlement
CREATE TABLE pesalink_recipients (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id      UUID NOT NULL REFERENCES merchants(id),
    currency         TEXT NOT NULL,
    account_hash     TEXT NOT NULL, -- hash of account details
    pesalink_acct_id TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (merchant_id, currency, account_hash)
);
```

---

## 15. Testing Strategy

| Layer | What | How |
|---|---|---|
| **Domain** | State machine transitions, validation, AttemptStatus mapping | Pure Go tests, no deps, table-driven |
| **Repository** | SQL queries | Testcontainers with Postgres 15 |
| **Orchestrator** | Routing + connector dispatch, failover, idempotency | Mock connector/router/repo interfaces |
| **Ledger** | Double-entry invariants | Property-based: random event sequences, assert sum(debits)==sum(credits) per currency |
| **Connector — M-Pesa** | STK Push, B2C, webhook parsing, error code mapping | Daraja sandbox + mock HTTP server for edge cases |
| **Connector — KCB** | STK/IPN flow, IPN parsing | KCB Buni UAT sandbox |
| **Connector — Paystack** | `/transaction/initialize`, webhook HMAC, `/transaction/verify` | Paystack test mode |
| **Connector — Pesalink** | Quote→Recipient→Transfer→Fund chain, recipient caching, transfer webhook | Pesalink sandbox |
| **E2E** | Full payment flow per method | Docker Compose with Postgres + Redis + NATS, mock PSP servers |
| **Chaos** | Connector timeout → failover, duplicate webhooks, out-of-order callbacks | Test harness injecting failures at connector boundary |
| **Idempotency** | Same request twice, same webhook twice | Fire duplicates, assert single side effect and no ledger duplication |

**Key invariants tested at every layer:**
- A `PaymentIntent` never regresses from `succeeded`
- Every `succeeded` transition posts exactly 2+ balanced ledger entries
- Duplicate webhooks with the same `dedup_key` are idempotent
- Routing failover on connector timeout selects the next available PSP
- Idempotency key replay returns the same result without side effects
- Pesalink `InitiateCollection` always returns an error (never silently accepted)
- `sum(debits) == sum(credits)` per currency across all ledger entries at any point in time

---

## 16. Implementation Sequence

| Phase | Packages | Milestone |
|---|---|---|
| **P0 — Skeleton** | `cmd/api`, `internal/domain`, `internal/repository`, `internal/api`, `migrations` | Empty server boots, health check returns 200, migrations run cleanly |
| **P1 — Core Flow** | `internal/connector` (interface + registry), `internal/orchestrator`, `internal/routing`, `internal/idempotency`, `internal/risk`, `pkg/phoneutil` | Create PI → route → simulate connector → state machine works; phone normalisation shared |
| **P2 — Ledger** | `internal/ledger`, `internal/repository/ledger.go` | Ledger entries posted on terminal PI transitions; invariant tests pass; V1 account hierarchy seeded |
| **P3 — Webhooks** | `internal/webhook`, `internal/repository/webhookevent.go`, `cmd/worker` | Webhook ingestion → dedup → persist → event bus consumer processes event; ack-fast-process-async confirmed |
| **P4a — M-Pesa** | `internal/connector/mpesa` | STK Push contract test passing in Daraja sandbox; B2C payout stub (production B2C Go-Live separate) |
| **P4b — KCB** | `internal/connector/kcb` | IPN flow working in Buni UAT; failover from M-Pesa to KCB exercised in E2E |
| **P4c — Paystack** | `internal/connector/paystack` | `initialize` + `verify` + webhook HMAC all passing against Paystack test mode |
| **P4d — Pesalink** | `internal/connector/pesalink`, `internal/settlement`, `pesalink_recipients` table | Quote→Recipient→Transfer→Fund chain working in sandbox; recipient caching tested; payout ledger entries correct |
| **P5 — Reconciliation** | `internal/reconciliation`, `cmd/cron` | Nightly reconciliation job per PSP; exception queue populated for mismatches |
| **P6 — Notification** | `internal/notification` | Outbound merchant webhooks with exponential-backoff retry |
| **P7 — Merchant Portal** | `internal/merchant`, `internal/authn`, frontend | API key management, merchant CRUD, routing rule management, dashboard data endpoints |
| **P8 — Checkout Widget** | `internal/checkout`, frontend widget | Public token endpoint, widget JS SDK (drives Paystack inline + M-Pesa STK UX) |
| **P9 — Stripe + PayPal** | `internal/connector/stripe`, `internal/connector/paypal` | Card and wallet expansion; MethodCard routing updated to try Stripe first |

---

## 17. PSP-Specific Production Go-Live Checklist

### M-Pesa (Daraja)
- [ ] STK Push Go-Live approved by Safaricom
- [ ] B2C Go-Live approved separately (for refunds — separate process from STK)
- [ ] Production `SecurityCredential` generated using Safaricom's production certificate (not sandbox cert)
- [ ] Safaricom egress IP allowlist confirmed and loaded into `MPESA_ALLOWED_IPS`
- [ ] Callback URL registered on Daraja production portal
- [ ] Nightly reconciliation job scheduled and exception alerting wired up

### KCB (Buni)
- [ ] Production credentials obtained from KCB Buni team
- [ ] Production IPN callback URL registered with KCB Buni team (email-based process)
- [ ] KCB production egress IPs confirmed and loaded into `KCB_ALLOWED_IPS`
- [ ] Multi-network test (Airtel Money, T-Kash) confirmed working end-to-end in UAT

### Paystack
- [ ] Live mode secret key (`sk_live_*`) obtained
- [ ] Webhook URL registered in Paystack dashboard (Settings → API Keys & Webhooks)
- [ ] Webhook signing secret loaded into `PAYSTACK_WEBHOOK_SECRET`
- [ ] KYC/business verification completed on Paystack dashboard

### Pesalink
- [ ] Business account verified and production API token issued
- [ ] Profile ID (`PESALINK_PROFILE_ID`) confirmed for production environment
- [ ] Pesalink RSA public key loaded for webhook signature verification
- [ ] Settlement currency pairs tested (KES→KES, KES→USD as applicable)
- [ ] Recipient caching table (`pesalink_recipients`) populated via settlement dry-run
