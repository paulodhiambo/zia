# Payment Aggregator / Switch — Technical Architecture (V1)

**Stack:** Go (Golang)
**Forward-looking requirement:** the core must expose a session/token-based API that a future embeddable JS widget can drive without redesign.

---

## 1. Goals & Non-Goals

### 1.1 Goals
- One canonical **Payment Intent** model that abstracts away provider-specific quirks (STK push vs. redirect vs. hosted fields vs. webhook-only confirmation).
- Pluggable **connector** architecture — adding PSP #7 should never touch core orchestration code.
- Built for **idempotency, auditability, and reconciliation** from day one — this is money, not a CRUD app.
- Designed so the *checkout session* concept introduced now becomes the backend for the embeddable widget later, with no breaking API changes.

### 1.2 Non-Goals (V1)
- Becoming a licensed deposit-taking institution or e-money issuer — this system is a **switch/orchestrator**, not a bank. Funds custody/settlement timing must respect each PSP's and Kenya's regulatory model (see §15).
- Card data custody — no raw PAN ever touches our servers (we use Stripe Elements / Paystack inline / PayPal SDK tokenization at the edge; see §10).
- Multi-region active-active from day one (designed for, not built for, in V1).

---

## 2. Glossary

| Term | Meaning |
|---|---|
| **Switch** | The core orchestration engine that routes a payment request to the right PSP connector |
| **Connector / Adapter** | Go package implementing a common interface per PSP |
| **Payment Intent** | Our canonical, provider-agnostic record of "merchant wants to collect X amount from a customer" |
| **Charge / Attempt** | One concrete try against one PSP for one Payment Intent (a Payment Intent can have multiple Attempts on retry/failover) |
| **Checkout Session** | A short-lived, pre-configured object the future widget will reference by a public token to render a checkout UI |
| **IPN/Callback/Webhook** | Asynchronous, server-to-server notification of a payment event from a PSP |

---

## 3. High-Level Architecture

```mermaid
flowchart TB
    subgraph Merchant["Merchant Side"]
        MAPP[Merchant Backend / Future Widget]
    end

    subgraph Edge["Edge Layer"]
        LB[Load Balancer / WAF]
        GW[API Gateway<br/>AuthN, rate limit, request signing]
    end

    subgraph Core["Core Services (modular monolith, V1)"]
        MS[Merchant & API-Key Service]
        CS[Checkout Session Service]
        ORC[Orchestration Engine<br/>the Switch]
        RTE[Routing Engine]
        IDP[Idempotency Service]
        LED[Ledger Service<br/>double-entry]
        REC[Reconciliation Service]
        WH[Webhook Ingestion Service]
        NOT[Notification / Merchant-Webhook Dispatcher]
        FRD[Risk & Fraud Rules Engine]
        SET[Settlement & Payout Service]
    end

    subgraph Conn["Connector Layer"]
        CMP[M-Pesa Connector]
        CPP[PayPal Connector]
        CPS[Paystack Connector]
        CST[Stripe Connector]
    end

    subgraph Infra["Infrastructure"]
        PG[(PostgreSQL<br/>system of record)]
        RDS[(Redis<br/>cache/idempotency/locks)]
        MQ[(Kafka/NATS JetStream<br/>event bus)]
        VLT[(Vault / KMS<br/>secrets, encryption keys)]
        OBS[(Prometheus/Grafana/Loki/Tempo)]
    end

    MAPP -->|REST + HMAC signed| LB --> GW
    GW --> MS
    GW --> CS
    GW --> ORC
    ORC --> RTE
    ORC --> IDP
    ORC --> FRD
    ORC --> Conn
    EXT -->|webhooks/callbacks| WH
    WH --> MQ
    MQ --> ORC
    MQ --> LED
    MQ --> NOT
    NOT -->|merchant webhook| MAPP
    ORC --> LED
    LED --> REC
    SET --> CWS
    SET --> LED
    Core --> PG
    Core --> RDS
    Core --> VLT
    Core -.-> OBS
```

**Why a modular monolith for V1, not microservices?** A payment switch's hardest problems (idempotency, ledger consistency, transactional integrity across "charge the customer" + "record the ledger entry" + "notify the merchant") get *easier*, not harder, inside one deployable unit with one database transaction boundary, while you're still validating product-market fit and routing logic. Each box under **Core Services** above is a separate Go package with a clean interface and **owns its own DB schema/tables** — so it can be peeled off into its own microservice later (the **Connector Layer is already isolated this way for the exact same reason — PSP outages must never cascade**) without a rewrite. This is the standard "modular monolith → strangler-fig to microservices" path used by most successful payment platforms in their first 1–2 years.

---

## 4. Core Domain Model

The single most important architectural decision is the **state machine separation between three concepts**:

1. **PaymentIntent** — what the merchant *asked for* (amount, currency, merchant, customer, desired methods). Survives retries.
2. **Attempt (Charge)** — one concrete try with one specific PSP. A PaymentIntent can have N attempts (e.g., Stripe card declined → fallback to Paystack).
3. **LedgerEntry** — the immutable, append-only financial record. Only written once money *actually* moves, never speculatively.

```mermaid
erDiagram
    MERCHANT ||--o{ PAYMENT_INTENT : creates
    PAYMENT_INTENT ||--o{ ATTEMPT : has
    ATTEMPT ||--o| WEBHOOK_EVENT : "confirmed by"
    ATTEMPT ||--o{ LEDGER_ENTRY : posts
    PAYMENT_INTENT ||--o{ REFUND : has
    REFUND ||--o{ LEDGER_ENTRY : posts
    MERCHANT ||--o{ PSP_ACCOUNT : "onboarded on"
    MERCHANT ||--o{ PAYOUT : receives
    PAYOUT ||--o{ LEDGER_ENTRY : posts
    PAYMENT_INTENT ||--o| CHECKOUT_SESSION : "rendered via"

    MERCHANT {
        uuid id PK
        string legal_name
        string country
        string default_currency
        string status
        jsonb settlement_config
    }
    PAYMENT_INTENT {
        uuid id PK
        uuid merchant_id FK
        bigint amount_minor
        string currency
        string status
        string customer_ref
        jsonb allowed_methods
        timestamptz expires_at
        timestamptz created_at
    }
    ATTEMPT {
        uuid id PK
        uuid payment_intent_id FK
        string psp
        string psp_reference
        string status
        jsonb raw_request
        jsonb raw_response
        int sequence_no
        timestamptz created_at
    }
    LEDGER_ENTRY {
        uuid id PK
        uuid account_id FK
        string entry_type
        bigint amount_minor
        string currency
        string reference_type
        uuid reference_id
        timestamptz posted_at
    }
    WEBHOOK_EVENT {
        uuid id PK
        string psp
        string event_type
        string dedup_key
        jsonb payload
        string processing_status
        timestamptz received_at
    }
    REFUND {
        uuid id PK
        uuid payment_intent_id FK
        bigint amount_minor
        string status
    }
    PSP_ACCOUNT {
        uuid id PK
        uuid merchant_id FK
        string psp
        jsonb credentials_ref
        string status
    }
    PAYOUT {
        uuid id PK
        uuid merchant_id FK
        bigint amount_minor
        string currency
        string rail
        string status
    }
    CHECKOUT_SESSION {
        uuid id PK
        uuid payment_intent_id FK
        string public_token
        jsonb ui_config
        timestamptz expires_at
    }
```

### 4.1 Payment Intent state machine

```mermaid
stateDiagram-v2
    [*] --> created
    created --> requires_action: PSP needs customer action (PIN entry, 3DS, redirect)
    created --> processing: PSP accepted, awaiting async confirmation
    requires_action --> processing
    processing --> succeeded: webhook/poll confirms success
    processing --> failed: webhook/poll confirms failure
    processing --> requires_action
    created --> failed: PSP rejected at initiation
    failed --> created: merchant/system retries with new Attempt (failover)
    succeeded --> partially_refunded
    succeeded --> refunded
    failed --> expired: TTL exceeded
    created --> expired: TTL exceeded with no attempt
```

Key rule: **a PaymentIntent never regresses out of `succeeded`** except through an explicit, separately-ledgered Refund. This protects the ledger from being mutated by late/duplicate webhooks.

### 4.2 Core Go types (system of record, simplified)

```go
package domain

type Currency string // ISO 4217, e.g. "KES", "USD"

type PaymentIntentStatus string

const (
    StatusCreated         PaymentIntentStatus = "created"
    StatusRequiresAction  PaymentIntentStatus = "requires_action"
    StatusProcessing      PaymentIntentStatus = "processing"
    StatusSucceeded       PaymentIntentStatus = "succeeded"
    StatusFailed          PaymentIntentStatus = "failed"
    StatusExpired         PaymentIntentStatus = "expired"
    StatusPartiallyRefund PaymentIntentStatus = "partially_refunded"
    StatusRefunded        PaymentIntentStatus = "refunded"
)

// PaymentIntent is the merchant-facing, provider-agnostic record.
// AmountMinor is always in the smallest currency unit (cents, lowest KES unit)
// to avoid float arithmetic anywhere in the system.
type PaymentIntent struct {
    ID              string
    MerchantID      string
    AmountMinor     int64
    Currency        Currency
    Status          PaymentIntentStatus
    CustomerRef     string
    CustomerPhone   string // for M-Pesa STK
    CustomerEmail   string // for Stripe/PayPal/Paystack
    AllowedMethods  []PaymentMethod
    IdempotencyKey  string
    MetadataJSON    []byte
    ExpiresAt       time.Time
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

type PaymentMethod string

const (
    MethodMpesaSTK   PaymentMethod = "mpesa_stk"
    MethodCard       PaymentMethod = "card"           // routed to Stripe/Paystack/PayPal
    MethodPaypal     PaymentMethod = "paypal_redirect"
)

// Attempt represents one concrete try against one PSP for one PaymentIntent.
type Attempt struct {
    ID              string
    PaymentIntentID string
    PSPReference    string // CheckoutRequestID, PaymentIntent ID, OrderID, etc.
    Status          string
    SequenceNo      int    // for ordering failover attempts
    RawRequest      []byte
    RawResponse     []byte
    CreatedAt       time.Time
    UpdatedAt       time.Time
}

// LedgerEntry is append-only. Never updated, never deleted.
// Double-entry: every business event posts >=2 balanced entries.
type LedgerEntry struct {
    ID            string
    AccountID     string // e.g. "merchant:<id>:available", "psp_clearing:stripe", "platform:fees"
    EntryType     string // "debit" | "credit"
    AmountMinor   int64
    Currency      Currency
    ReferenceType string // "attempt" | "refund" | "payout" | "fee"
    ReferenceID   string
    PostedAt      time.Time
}
```

---

## 5. PSP Connector Layer — the Adapter Pattern


```go
package connector

import "context"

// Connector is implemented once per PSP. Adding a new PSP means writing
// one new file that satisfies this interface — the orchestration engine,
// routing engine, and ledger never change.
type Connector interface {
    // Name returns the canonical PSP identifier, e.g. "mpesa".
    Name() string

    // Capabilities tells the routing engine what this connector can do,
    // so routing decisions are made on real capability, not assumption.
    Capabilities() Capabilities

    // InitiateCollection starts a customer-money-in flow.
    // For STK-style PSPs this triggers the push and returns immediately
    // with status=requires_action; confirmation comes later via webhook.
    // For Stripe/PayPal this creates the PaymentIntent/Order server-side.
    InitiateCollection(ctx context.Context, req CollectionRequest) (CollectionResult, error)

    // GetStatus actively polls the PSP — used as a reconciliation
    // fallback when webhooks are missed (critical for M-Pesa).
    GetStatus(ctx context.Context, pspReference string) (StatusResult, error)

    // Refund issues money back to the customer.
    Refund(ctx context.Context, req RefundRequest) (RefundResult, error)

    // InitiatePayout moves money OUT to a merchant or customer
    InitiatePayout(ctx context.Context, req PayoutRequest) (PayoutResult, error)

    // ParseWebhook verifies signature/authenticity and normalizes the
    // PSP's native callback payload into our canonical WebhookEvent.
    ParseWebhook(ctx context.Context, headers map[string]string, body []byte) (WebhookEvent, error)
}

type Capabilities struct {
    SupportsCollection bool
    SupportsPayout     bool
    SupportsRefund     bool
    SupportedCurrencies []string
    SupportedCountries  []string
    ConfirmationStyle   string // "synchronous" | "webhook_only" | "redirect_then_webhook"
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
    Status       string // maps to Attempt.Status
    NextAction   *NextAction // e.g. redirect URL for PayPal, or nil for STK (PIN prompt is on-device)
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

### 5.1 Per-PSP connector notes

| PSP | Primary use in V1 | Auth model | Confirmation style | Key implementation notes |
|---|---|---|---|---|
| **M-Pesa (Daraja 3.0)** | Customer collection (KE) | OAuth2 client-credentials, token cached ~55 min | STK Push is async: initiate → customer enters PIN on-device → Safaricom POSTs callback. No customer-visible redirect. | Use **C2B STK Push (Lipa Na M-Pesa Online)** for collection; **B2C** for refunds/payouts (separate Go-Live approval & `SecurityCredential`). Always run a **nightly Transaction Status reconciliation job** — callbacks are not 100% guaranteed delivery. Phone numbers normalized to `2547XXXXXXXX`. Respond `HTTP 200` to every callback within 30s, even on internal processing errors, to avoid Safaricom's aggressive retry storm; ack-then-process-async via the event bus. |
| **PayPal** | Customer collection (intl) | OAuth2 client-credentials | Orders API v2: create order → customer approves (redirect or PayPal JS SDK button) → capture server-side. Webhooks confirm `CHECKOUT.ORDER.APPROVED` / `PAYMENT.CAPTURE.COMPLETED` | `NextAction.Type = "redirect"` (or embed via SDK once widget exists). Verify webhook signature via PayPal's transmission-signature verification API, not just shared secret. |
| **Paystack** | Customer collection (cards + mobile money, pan-African) | Secret key, Bearer auth | `POST /transaction/initialize` → redirect to Paystack-hosted page or inline popup → `charge.success` webhook is source of truth (poll `/transaction/verify/:reference` as fallback) | Verify webhook via `x-paystack-signature` HMAC-SHA512. Good fallback/expansion rail for African card traffic alongside Stripe. |
| **Stripe** | Customer collection (cards/wallets, global) | Secret key, Bearer auth | `PaymentIntents` API; client uses Stripe.js/Elements (tokenizes card client-side — **no PAN touches our servers**); `payment_intent.succeeded` webhook | Verify webhook via `Stripe-Signature` header + signing secret, with timestamp tolerance to prevent replay. This is the reference implementation for "PCI scope stays with the PSP." |

---

## 6. Orchestration Engine ("the Switch")

This is the only component allowed to change a `PaymentIntent`'s status. It is intentionally a thin, deterministic state-machine executor — all "cleverness" (which PSP to use, retry policy) is delegated to the **Routing Engine** so the orchestration logic itself stays simple and auditable.

```mermaid
sequenceDiagram
    participant M as Merchant Backend
    participant GW as API Gateway
    participant ORC as Orchestrator
    participant IDP as Idempotency Svc
    participant RTE as Routing Engine
    participant CON as PSP Connector
    participant PSP as External PSP
    participant LED as Ledger
    participant MQ as Event Bus

    M->>GW: POST /v1/payment_intents (Idempotency-Key header)
    GW->>ORC: CreatePaymentIntent(req)
    ORC->>IDP: Check idempotency key
    IDP-->>ORC: Not seen before -> proceed
    ORC->>RTE: SelectConnector(currency, country, method, merchant prefs)
    ORC->>CON: InitiateCollection(req)
    CON->>PSP: STK Push request (signed/OAuth)
    PSP-->>CON: CheckoutRequestID
    CON-->>ORC: Attempt{status=requires_action, psp_ref}
    ORC->>ORC: PaymentIntent.status = requires_action
    ORC-->>M: 201 {id, status: requires_action}

    Note over PSP,M: Customer enters M-Pesa PIN on phone

    PSP->>GW: POST /webhooks/mpesa (callback)
    GW->>ORC: dispatch to Webhook Ingestion Service (see §8)
    ORC->>ORC: PaymentIntent.status = succeeded (idempotent transition)
    ORC->>LED: Post balanced ledger entries
    ORC->>MQ: publish payment.succeeded
    MQ->>M: Notification Service delivers signed merchant webhook
```

### 6.1 Failover logic (within Orchestrator + Routing Engine)

If `Attempt[n]` fails for a *retryable* reason (PSP timeout, 5xx, insufficient float at PSP, generic decline that isn't a hard customer-side decline), the Orchestrator asks the Routing Engine for the **next** candidate connector and creates `Attempt[n+1]` against the **same** `PaymentIntent`. Hard failures (insufficient funds, wrong PIN 3x, card declined as fraud) are **not** retried automatically — they're surfaced to the merchant/customer to act on.

---

## 7. Routing Engine

Routing decisions are config-driven (DB-backed, hot-reloadable), not hardcoded, because routing rules change far more often than code should deploy.

```go
package routing

type RouteRequest struct {
    MerchantID string
    Currency   string
    Country    string
    Method     string // requested method, or "" = let engine decide
    AmountMinor int64
}

type RouteDecision struct {
    Primary   string   // connector name
    Fallbacks []string // ordered fallback chain
    Reason    string   // for observability/audit
}

// Router applies rules in priority order. V1 starts rule-based;
// success-rate-weighted scoring is a natural V2 evolution using the
// same interface (so callers never change).
type Router interface {
    Route(ctx context.Context, req RouteRequest) (RouteDecision, error)
}
```

**V1 rule set (illustrative, stored as data not code):**

| Priority | Rule |
|---|---|
| 1 | Merchant has an explicit PSP override for this currency/method → use it |
| 3 | `currency=KES` + `method=card` → primary `paystack`, fallback `stripe` |
| 4 | International card traffic → primary `stripe`, fallback `paystack` (where supported) or `paypal` |
| 5 | Customer explicitly chose PayPal at checkout → `paypal`, no fallback (PayPal customers expect PayPal) |
| 7 | Default catch-all → highest-priority connector that lists the currency in `Capabilities.SupportedCurrencies` |


---

## 8. Webhook Ingestion Architecture

Webhooks are the most failure-prone part of any payment switch (replay attacks, out-of-order delivery, duplicate delivery, PSP retries with no idempotency on their side). The design below assumes **all of the above will happen**.

```mermaid
flowchart LR
    A[PSP sends webhook] --> B[Edge endpoint<br/>/webhooks/:psp]
    B --> C{Signature valid?}
    C -- no --> D[401, log, alert if rate spikes]
    C -- yes --> E[Connector.ParseWebhook<br/>normalize to WebhookEvent]
    E --> F{dedup_key seen<br/>in last 7 days?}
    F -- yes --> G[200 OK, no-op<br/>idempotent ack]
    F -- no --> H[Persist WebhookEvent<br/>status=received]
    H --> I[200 OK immediately<br/>ack within PSP timeout]
    H --> J[Publish to event bus<br/>async]
    J --> K[Orchestrator consumer<br/>applies state transition]
    K --> L{Transition valid<br/>per state machine?}
    L -- yes --> M[Update PaymentIntent<br/>+ Ledger, mark event processed]
    L -- no e.g. already terminal --> N[Mark event ignored,<br/>log for audit]
    M --> O{Processing failed?}
    O -- yes --> P[Retry with backoff,<br/>then Dead Letter Queue]
```

**Non-negotiable rules:**
1. **Verify signature before doing anything else** — HMAC (Paystack), `Stripe-Signature` (Stripe), transmission-signature API (PayPal), and IP-allowlist + shared-secret/IPN-token checks for M-Pesa which don't sign payloads as strongly.
2. **Always ACK fast, process async.** Safaricom in particular retries aggressively on non-200 or slow responses — the edge handler's only job is verify → dedupe → persist → 200. Business logic happens in a consumer off the event bus.
3. **Dedup key** = PSP's own event/transaction ID when available, else a stable hash of `(psp, pspReference, eventType, amount)`.
4. **State machine guards every transition** — a duplicate or late "succeeded" webhook arriving after a Refund has already been processed is a no-op, never a regression.
5. **Reconciliation job** (cron, hourly + full nightly run) calls `Connector.GetStatus` for every `Attempt` still `processing` past a threshold — this is what protects against missed webhooks entirely, which *will* happen with M-Pesa at some point.

---

## 9. Ledger & Settlement

### 9.1 Double-entry ledger
Every money movement posts **at least two balanced LedgerEntry rows** (sum of debits = sum of credits), e.g.:

- Customer pays KES 1,000 via M-Pesa → `debit psp_clearing:mpesa 1000` / `credit merchant:<id>:available 1000` minus platform fee, which itself is `debit merchant:<id>:available <fee>` / `credit platform:fees <fee>`.
- This is the same model used by Stripe's own internal ledger and most serious fintech ledgers — it makes "where did the money go" a query, not an investigation, and it's what your auditors/regulators will ask for first.

### 9.2 Settlement & Payout Service
Decoupled from the collection path entirely. Runs on a schedule (e.g. T+1, configurable per merchant) or on-demand:
1. Sum each merchant's `available` ledger balance per currency.

### 9.3 Reconciliation Service
Nightly job per PSP:
- Match against our `Attempt`/`Payout` records by `psp_reference`.
- Flag: amount mismatches, orphaned PSP transactions (PSP has it, we don't — usually a missed webhook), orphaned local transactions (we have it, PSP doesn't — usually a failed-but-recorded-as-success bug) into an **exceptions queue** a human reviews. This single job is what catches the bugs that "passed all tests."

---

## 10. Security Architecture

| Layer | Control |
|---|---|
| **PCI DSS scope minimization** | Card PAN never reaches our backend. Stripe Elements / Paystack inline-iframe / PayPal SDK tokenize client-side; we only ever see PSP-issued tokens/PaymentIntent IDs. This keeps us at **SAQ A** scope, not full PCI DSS Level 1 infra. |
| **Secrets** | All PSP API keys/credentials/`SecurityCredential` (M-Pesa B2C cert) stored in **Vault/KMS**, never in env vars in plaintext in production, fetched at runtime with short-lived leases, rotated on a schedule and immediately on suspected compromise. |
| **Transport** | TLS 1.2+ everywhere; mTLS between internal services once split beyond the monolith. |
| **Merchant API auth** | API key (public/secret pair) + **HMAC request signing** (timestamp + nonce + body hash) to prevent replay, mirroring how the future widget's public tokens will work. |
| **Webhook auth** | Per-PSP signature verification (§8) + IP allowlisting where the PSP publishes static egress IPs (M-Pesa). |
| **Data at rest** | Column-level encryption for PII (phone numbers, emails) using envelope encryption via KMS; full-disk encryption on all volumes. |
| **AuthZ** | RBAC for internal dashboard/ops users; merchants are tenant-isolated at the query layer (every query scoped by `merchant_id`, enforced via Postgres Row-Level Security as a second line of defense). |
| **Audit trail** | Every state transition, every webhook received, every manual ops action is append-only logged with actor + reason — required for dispute handling and regulator requests. |

---

## 11. Tech Stack

| Concern | Choice | Why |
|---|---|---|
| Language | Go 1.23+ | Concurrency model fits high-throughput I/O-bound webhook/PSP traffic; static typing matters a lot for money code |
| HTTP framework | `chi` or `net/http` + middleware (avoid heavy frameworks; payment APIs benefit from explicit, boring code) | Minimal magic, easy to audit |
| Database | PostgreSQL | ACID transactions for ledger integrity; JSONB for flexible PSP raw-payload storage; mature, boring, well-understood |
| Cache / locks / idempotency store | Redis | Idempotency key cache, distributed locks for "don't double-process this webhook", rate limiting |
| Event bus | NATS JetStream (or Kafka if you anticipate very high volume / need long retention + replay for audit) | Decouples webhook ingestion from orchestration; enables retry/DLQ semantics natively |
| Secrets/KMS | HashiCorp Vault (or cloud KMS — AWS KMS/GCP KMS) | Centralized, audited secret access |
| Background jobs/cron | `river` (Postgres-backed queue) or a k8s CronJob runner | Reconciliation, settlement runs, token refresh |
| Observability | OpenTelemetry → Prometheus (metrics) + Loki (logs) + Tempo/Jaeger (traces) + Grafana (dashboards) | Trace a single payment across connector calls, webhook processing, ledger posting |
| Deployment | Docker + Kubernetes (or a managed PaaS for V1 to reduce ops burden — e.g. ECS/Fly.io/Render before justifying full K8s) | Start as simply as you can defend; the modular monolith doesn't need K8s on day one |
| CI/CD | GitHub Actions, contract tests against PSP sandboxes, mandatory ledger-balance invariant tests | A failed "debits == credits" test should block deploy, full stop |

---

## 12. Suggested Go Project Structure

```
payswitch/
├── cmd/
│   ├── api/                  # main HTTP server entrypoint
│   ├── worker/               # event bus consumers (webhooks, notifications)
│   └── cron/                 # reconciliation, settlement, token-refresh jobs
├── internal/
│   ├── domain/                # core types: PaymentIntent, Attempt, LedgerEntry, etc.
│   ├── orchestrator/          # the Switch — state machine, no PSP-specific code
│   ├── routing/                # routing engine + rule storage
│   ├── idempotency/
│   ├── ledger/                 # double-entry posting logic, balance queries
│   ├── reconciliation/
│   ├── settlement/
│   ├── webhook/                 # ingestion, dedup, dispatch
│   ├── notification/             # merchant-facing webhook dispatcher with retry/backoff
│   ├── risk/                      # rules engine (velocity checks, amount thresholds, blocklists)
│   ├── checkout/                   # Checkout Session service (widget-facing, see §13)
│   ├── merchant/                    # merchant + API key management
│   └── connector/
│       ├── connector.go              # the Connector interface (§5)
│       ├── mpesa/
│       ├── paypal/
│       ├── paystack/
│       ├── stripe/
├── pkg/
│   ├── httpsign/                       # HMAC request signing helpers (shared client+server)
│   └── moneyutil/                       # minor-unit arithmetic helpers, no floats anywhere
├── migrations/
├── deploy/
│   ├── docker/
│   └── k8s/
└── test/
    ├── contract/                          # per-PSP sandbox contract tests
    └── e2e/
```

Each `connector/<psp>/` package is self-contained: its own HTTP client, its own auth/token management, its own webhook parser, and a single exported type satisfying `connector.Connector`. This is what makes "PSP #7" a single new folder, not a cross-cutting change.

---

## 13. API Design (merchant-facing) — and the on-ramp to the widget

### 13.1 Core REST endpoints (V1)

| Method & Path | Purpose |
|---|---|
| `POST /v1/payment_intents` | Create a payment intent (the merchant's backend calls this) |
| `GET /v1/payment_intents/:id` | Poll status |
| `POST /v1/payment_intents/:id/confirm` | For methods needing an explicit confirm step (e.g. after redirect-based PayPal approval) |
| `POST /v1/payment_intents/:id/refunds` | Full/partial refund |
| `GET /v1/transactions` | List/filter, for merchant reconciliation/dashboards |
| `POST /v1/checkout_sessions` | **Forward-looking:** creates a Checkout Session + short-lived public token the widget will use |
| `GET /v1/checkout_sessions/:token` | **Public, unauthenticated** (token-scoped) — what the widget calls to render the right UI/methods |
| `POST /v1/webhooks/:psp` | Inbound, PSP→us |
| Merchant-configured webhook URL | Outbound, us→merchant, HMAC-signed, with exponential-backoff retry + a "replay this event" ops tool |

### 13.2 Designing for the widget *now*

Since the widget is coming next, the **Checkout Session** abstraction is introduced in V1 specifically so the widget integration later requires **zero backend redesign** — only a new frontend:

1. Merchant backend creates a `PaymentIntent`, then a `CheckoutSession` referencing it, gets back a `public_token` (short TTL, single-use, no merchant secret embedded).
2. Merchant's page embeds `<script src=".../widget.js" data-token="...">` (future deliverable).
3. Widget calls `GET /v1/checkout_sessions/:token` — a **public**, rate-limited, token-scoped endpoint — to learn which methods to render (e.g. "this customer is in Kenya, show M-Pesa + card").
4. Widget collects only what each method needs (phone number for STK, or hands off to Stripe.js/Paystack inline/PayPal SDK for tokenized card/wallet entry) and calls `confirm` — never touching merchant secret keys, never touching raw card data.
5. Widget polls or opens a WebSocket/SSE channel scoped to the session for real-time status (critical for STK push's "waiting for PIN entry" UX) — design the `CheckoutSession` table and event bus topic now so this is additive later, not retrofitted.

This is exactly the **Stripe PaymentIntent + Stripe.js / Elements pattern** and the **Paystack inline popup pattern** — proven, and it composes naturally with the multi-PSP router underneath since the widget never needs to know which PSP actually services a given method.

---

## 14. Risk & Fraud (V1 — rules-based, not ML)

A lightweight `risk` module runs synchronously before `InitiateCollection`:
- Velocity checks (same customer phone/email/IP exceeding N attempts per minute/hour).
- Amount-threshold review queue (large transactions flagged for manual review before settlement, not before collection — don't block legitimate customers).
- Basic deny-list (phone numbers/emails/cards previously charged back or confirmed fraudulent).
- Per-merchant configurable risk thresholds, since a KES 50 merchant and a KES 5M merchant have very different "normal."

This is intentionally simple for V1; the `risk.Engine` interface should be designed so a future ML scoring service is just a new implementation behind the same interface.

---

## 15. Compliance & Regulatory Considerations

> This is **not legal advice** — confirm specifics with Kenyan counsel and each PSP's partnership team before going live. The points below are architectural implications of real, well-known regulatory facts.

- **CBK / National Payment System Act:** Operating a switch that touches M-Pesa rails for *other merchants* (i.e., as a payment service provider/aggregator, not your own e-commerce checkout) typically requires registration/authorization with the **Central Bank of Kenya** under the National Payment System Act/Regulations once you go beyond a single-merchant integration. Architect merchant onboarding/KYC (§ below) and segregated client-fund ledgering now so you're not retrofitting compliance later.
- **Segregated/Pass-through funds:** Treat all merchant collections as **client funds, not platform revenue**, on the ledger (separate top-level account hierarchy: `platform:*` vs `merchant:*:available`). This is both good architecture and typically a regulatory expectation for aggregators.
- **PCI DSS:** SAQ A scope as long as no raw card data is ever transmitted/stored/processed on our infra (enforced by always using PSP-hosted tokenization — §10).
- **KYC/AML on merchant onboarding:** A `merchant.onboarding` module (out of core scope for this document, but a hard dependency before go-live) collecting business registration, beneficial ownership, and screening against sanctions lists before a `PSP_ACCOUNT` is activated.
- **Data residency:** If regulation requires Kenyan transaction data to stay in-country, design the data layer so PII/transaction records for KE-rail payments can be pinned to an in-region DB without architectural rework (i.e., don't hardcode a single global Postgres instance into application logic — go through a repository layer).
- **GDPR / data protection (Kenya's DPA 2019 mirrors much of GDPR):** PII encryption (§10), data subject deletion/export support designed into the schema from day one (don't hard-delete ledger entries — use anonymization where legally required for older PII while preserving immutable financial records).

---

## 16. Observability & SRE

- **Distributed tracing** (OpenTelemetry): one trace ID per `PaymentIntent`, propagated through every Attempt, webhook, and ledger post — so "what happened to payment X" is one Grafana/Tempo query, not a grep across five services.
- **Key metrics:** success rate per PSP per method, P50/P95/P99 time-to-confirmation per PSP (STK push will be much slower than card capture — alert thresholds must differ per method), webhook processing lag, reconciliation exception count, circuit-breaker state per connector.
- **Alerting:** PagerDuty/Slack on: webhook signature-failure spikes (possible attack), reconciliation exceptions above threshold, any connector circuit breaker opening, ledger imbalance (debits ≠ credits — this should be a **page-immediately** severity-1, not a dashboard).
- **SLOs:** define explicit targets, e.g. "99.9% of webhook events processed within 5s of receipt," "99.5% of payment intents reach a terminal state within their PSP's expected window."

---

## 17. Testing Strategy

- **Ledger invariant tests:** property-based tests asserting sum(debits) == sum(credits) after every simulated event sequence, including out-of-order and duplicate webhook delivery.
- **Chaos/failover tests:** simulate a PSP timing out or returning 5xx mid-flow and assert the Routing Engine correctly fails over and the customer is never double-charged.
- **Idempotency tests:** fire the same merchant request / same webhook twice (and out of order) and assert single ledger effect.
- **Webhook replay tooling:** an internal ops endpoint to replay a stored webhook payload against a sandbox-flagged merchant, for support/debugging.

---

## 18. Roadmap Beyond V1

| Phase | Scope |
|---|---|
| **V1 (this doc)** | 6 PSPs, server-to-server API, modular monolith, rule-based routing |
| **V2** | Embeddable JS widget (built on the Checkout Session API above), hosted checkout page fallback for merchants without frontend dev resources |
| **V3** | Success-rate/cost-weighted smart routing (data-driven, same `Router` interface), additional PSPs/rails (e.g. Airtel Money direct, additional African card processors), merchant self-serve dashboard |
| **V4** | Extract Connector Layer and Ledger Service into independently deployed services if/when scale demands it; multi-region active-active; on-prem/private connectivity options for large merchants |

---

## 19. Summary of Key Architectural Decisions

1. **Provider-agnostic core, provider-specific edges** — the `Connector` interface is the seam that keeps the system extensible.
2. **Three-tier state model** (PaymentIntent → Attempt → LedgerEntry) so retries/failover never corrupt financial truth.
3. **Modular monolith now, microservices-ready later** — boundaries are real (separate packages/schemas), deployment is simple.
4. **Webhooks are untrusted input** — verify, dedupe, ack-fast-process-async, and never trust them as the *only* source of truth (reconciliation jobs are mandatory, not optional, especially for M-Pesa).
6. **Checkout Sessions exist from V1** specifically so the embeddable widget is a frontend project next, not a backend redesign.