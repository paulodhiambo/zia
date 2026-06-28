# Zia

**A unified payment aggregator/switch for Africa-first commerce.**

> "Zia" — Swahili for "cross over." Zia is the bridge merchants use to move money across payment rails without building and maintaining six separate integrations.

[![Go Version](https://img.shields.io/badge/go-1.23%2B-00ADD8)]()
[![License](https://img.shields.io/badge/license-Proprietary-lightgrey)]()
[![Status](https://img.shields.io/badge/status-pre--release-orange)]()

---

## Table of Contents

- [What is Zia?](#what-is-Zia)
- [Supported Payment Rails](#supported-payment-rails)
- [Architecture at a Glance](#architecture-at-a-glance)
- [Tech Stack](#tech-stack)
- [Project Structure](#project-structure)
- [Prerequisites](#prerequisites)
- [Getting Started](#getting-started)
- [Configuration](#configuration)
- [Running the Project](#running-the-project)
- [API Quickstart](#api-quickstart)
- [Authentication & Signing](#authentication--signing)
- [Webhooks](#webhooks)
- [Database & Migrations](#database--migrations)
- [Testing](#testing)
- [Observability](#observability)
- [Security](#security)
- [Roadmap](#roadmap)
- [Contributing](#contributing)
- [License](#license)
- [Support](#support)

---

## What is Zia?


Zia exists to solve three problems merchants in Kenya and across Africa hit immediately when accepting payments:

1. **Fragmentation** — every PSP has a different API shape, auth model, and confirmation flow (STK push vs. redirect vs. hosted fields vs. webhook-only).
2. **Reliability** — mobile money rails in particular drop webhooks and have float/downtime issues; a single integration with no failover means lost sales.
3. **Settlement complexity** — merchants want to get paid in the currency that's useful to them, not necessarily the currency the customer paid in.

For the full system design — domain model, state machines, connector interface, ledger design, routing logic, and compliance considerations — see [`ARCHITECTURE.md`](./ARCHITECTURE.md).

A **JavaScript embeddable checkout widget** is on the roadmap (see [Roadmap](#roadmap)) and is the reason the API already exposes a `CheckoutSession` resource — the backend won't need to change when the widget ships.

---

## Supported Payment Rails

| Provider | Role | Region | Flow |
|---|---|---|---|
| **M-Pesa** (Daraja) | Collection (primary, KE) + B2C refunds/payouts | Kenya | STK Push, async via callback |
| **Stripe** | Collection (cards/wallets) | Global | PaymentIntents, client-side tokenization |
| **Paystack** | Collection (cards + mobile money) | Africa-wide | Hosted/inline checkout, webhook-confirmed |
| **PayPal** | Collection (cards/wallet) | Global | Orders API v2, redirect/approve/capture |


---

## Architecture at a Glance

```mermaid
flowchart LR
    M[Merchant Backend] -->|REST + HMAC| GW[API Gateway]
    GW --> ORC[Orchestrator]
    ORC --> RTE[Routing Engine]
    ORC --> Conn[Connector Layer]
    PSP -->|webhooks| WH[Webhook Ingestion]
    WH --> ORC
    ORC --> LED[(Ledger - Postgres)]
    LED --> SET[Settlement Service]
    SET --> Conn
```

Full diagrams (ER model, state machines, sequence flows) live in [`ARCHITECTURE.md`](./ARCHITECTURE.md).

---

## Tech Stack

| Concern | Choice |
|---|---|
| Language | Go 1.23+ |
| HTTP | `chi` router |
| Database | PostgreSQL 15+ |
| Cache / idempotency / locks | Redis 7+ |
| Event bus | NATS JetStream |
| Secrets | HashiCorp Vault (local: `.env` + Docker secrets) |
| Background jobs | Postgres-backed queue (`river`) + cron runner |
| Observability | OpenTelemetry → Prometheus / Loki / Tempo / Grafana |
| Containerization | Docker, Docker Compose (local), Kubernetes (prod) |

---

## Project Structure

```
Zia/
├── cmd/
│   ├── api/          # HTTP API entrypoint
│   ├── worker/        # event bus consumers (webhooks, notifications)
│   └── cron/           # reconciliation, settlement, token-refresh jobs
├── internal/
│   ├── domain/          # PaymentIntent, Attempt, LedgerEntry types
│   ├── orchestrator/      # the Switch — core state machine
│   ├── routing/             # routing engine + rules
│   ├── idempotency/
│   ├── ledger/
│   ├── reconciliation/
│   ├── settlement/
│   ├── webhook/
│   ├── notification/
│   ├── risk/
│   ├── checkout/              # Checkout Session service (widget-facing)
│   ├── merchant/
│   └── connector/
│       ├── connector.go         # shared Connector interface
│       ├── mpesa/
│       ├── paypal/
│       ├── paystack/
│       ├── stripe/
├── pkg/
│   ├── httpsign/
│   └── moneyutil/
├── migrations/
├── deploy/
│   ├── docker/
│   └── k8s/
├── test/
│   ├── contract/
│   └── e2e/
├── docker-compose.yml
├── Makefile
├── .env.example
├── ARCHITECTURE.md
└── README.md
```

---

## Prerequisites

- Go 1.23 or later
- Docker & Docker Compose
- PostgreSQL 15+ (provided via Docker Compose for local dev)
- Redis 7+ (provided via Docker Compose for local dev)
- `make`
- Sandbox/developer accounts for the PSPs you intend to test:
    - [Safaricom Daraja](https://developer.safaricom.co.ke/) (M-Pesa)
    - [Stripe](https://dashboard.stripe.com/register) (test mode keys)
    - [Paystack](https://dashboard.paystack.com/#/signup) (test mode keys)
    - [PayPal Developer](https://developer.paypal.com/) (sandbox app)

---

## Getting Started

```bash
# 1. Clone the repo
git clone https://github.com/<org>/Zia.git
cd Zia

# 2. Copy environment template and fill in sandbox credentials
cp .env.example .env

# 3. Start dependencies (Postgres, Redis, NATS)
docker compose up -d postgres redis nats

# 4. Run database migrations
make migrate-up

# 5. Run the API server
make run-api

# 6. In a separate terminal, run the worker (webhook/event processing)
make run-worker
```

The API will be available at `http://localhost:8080` by default.

---

## Configuration

All configuration is via environment variables (see `.env.example` for the full list). Key groups:

```bash
# Core
APP_ENV=development
DATABASE_URL=postgres://Zia:Zia@localhost:5432/Zia?sslmode=disable
REDIS_URL=redis://localhost:6379/0
NATS_URL=nats://localhost:4222

# M-Pesa (Daraja)
MPESA_CONSUMER_KEY=
MPESA_CONSUMER_SECRET=
MPESA_SHORTCODE=
MPESA_PASSKEY=
MPESA_B2C_INITIATOR_NAME=
MPESA_B2C_SECURITY_CREDENTIAL=
MPESA_CALLBACK_BASE_URL=https://your-ngrok-or-domain.example.com


# Stripe
STRIPE_SECRET_KEY=
STRIPE_WEBHOOK_SIGNING_SECRET=

# Paystack
PAYSTACK_SECRET_KEY=
PAYSTACK_WEBHOOK_SECRET=

# PayPal
PAYPAL_CLIENT_ID=
PAYPAL_CLIENT_SECRET=
PAYPAL_WEBHOOK_ID=


# Security
VAULT_ADDR=
VAULT_TOKEN=
HMAC_SIGNING_SECRET=
```

> **Never commit `.env` or real credentials.** In staging/production, secrets are pulled from Vault/KMS at runtime — see `ARCHITECTURE.md §10`.

For local webhook testing against M-Pesa/Stripe/PayPal/Paystack sandboxes, expose your local server with a tunnel (e.g. `ngrok http 8080`) and register that URL as your callback/webhook URL in each PSP's dashboard.

---

## Running the Project

| Command | Description |
|---|---|
| `make run-api` | Starts the HTTP API server |
| `make run-worker` | Starts the event bus consumer (webhook + notification processing) |
| `make run-cron` | Starts scheduled jobs (reconciliation, settlement, token refresh) |
| `make migrate-up` | Applies pending DB migrations |
| `make migrate-down` | Rolls back the last migration |
| `make lint` | Runs `golangci-lint` |
| `make test` | Runs unit tests |
| `make test-contract` | Runs PSP sandbox contract tests (requires `.env` sandbox credentials) |
| `make test-e2e` | Runs end-to-end flow tests |
| `make docker-build` | Builds production Docker images |

---

## API Quickstart

### Create a payment intent (M-Pesa STK Push)

```bash
curl -X POST https://api.Zia.dev/v1/payment_intents \
  -H "Authorization: Bearer <YOUR_SECRET_KEY>" \
  -H "Idempotency-Key: $(uuidgen)" \
  -H "Content-Type: application/json" \
  -d '{
    "amount_minor": 50000,
    "currency": "KES",
    "method": "mpesa_stk",
    "customer_phone": "254712345678",
    "customer_ref": "order_8841"
  }'
```

```json
{
  "id": "pi_01HZX...",
  "status": "requires_action",
  "amount_minor": 50000,
  "currency": "KES",
  "method": "mpesa_stk"
}
```

The customer receives the M-Pesa PIN prompt on their phone. Zia updates the `PaymentIntent` to `succeeded` or `failed` asynchronously once Safaricom's callback (or the fallback reconciliation poll) confirms the outcome — your integration should rely on the **outbound webhook** or **polling `GET /v1/payment_intents/:id`**, not a synchronous response.

### Check status

```bash
curl https://api.Zia.dev/v1/payment_intents/pi_01HZX... \
  -H "Authorization: Bearer <YOUR_SECRET_KEY>"
```

### Issue a refund

```bash
curl -X POST https://api.Zia.dev/v1/payment_intents/pi_01HZX.../refunds \
  -H "Authorization: Bearer <YOUR_SECRET_KEY>" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{ "amount_minor": 50000 }'
```

### Create a checkout session (widget-ready, future-facing)

```bash
curl -X POST https://api.Zia.dev/v1/checkout_sessions \
  -H "Authorization: Bearer <YOUR_SECRET_KEY>" \
  -d '{ "payment_intent_id": "pi_01HZX..." }'
```

```json
{
  "public_token": "cs_pub_01J...",
  "expires_at": "2026-06-26T12:30:00Z"
}
```

Full endpoint reference lives in [`ARCHITECTURE.md §13`](./ARCHITECTURE.md#13-api-design-merchant-facing--and-the-on-ramp-to-the-widget) and (once published) the OpenAPI spec at `/docs/openapi.yaml`.

---

## Authentication & Signing

Every mutating request requires:
- `Authorization: Bearer <secret_key>` — issued per merchant, scoped to `sandbox` or `live`.
- `Idempotency-Key` header — a client-generated UUID; replaying the same key + payload returns the original result rather than creating a duplicate.
- HMAC request signing (timestamp + nonce + body hash) on top of the bearer token for write operations — see `pkg/httpsign` for the reference implementation and verification middleware.

Public, token-scoped endpoints (e.g. `GET /v1/checkout_sessions/:token`) do **not** require the merchant secret key — they're designed to be called directly from a browser/widget context.

---

## Webhooks

**Inbound** (PSP → Zia): `POST /webhooks/:psp` — signature-verified, deduplicated, acknowledged immediately, processed asynchronously. See `internal/webhook/`.

**Outbound** (Zia → Merchant): configured per merchant in the dashboard/API. Delivered with:
- `X-Zia-Signature` HMAC header for verification
- Exponential backoff retry (up to a configurable max) on non-2xx response
- An events log + manual "replay" tool for support/debugging

Example payload:

```json
{
  "event": "payment_intent.succeeded",
  "payment_intent_id": "pi_01HZX...",
  "amount_minor": 50000,
  "currency": "KES",
  "occurred_at": "2026-06-26T12:01:04Z"
}
```

---

## Database & Migrations

Migrations live in `migrations/` and are run via `make migrate-up` / `make migrate-down` (using `golang-migrate`). Schema changes affecting `ledger_entries` require a second reviewer and must include a corresponding update to the ledger invariant test suite (`sum(debits) == sum(credits)` must hold after every migration that touches financial tables).

---

## Testing

| Layer | Location | Notes |
|---|---|---|
| Unit tests | `internal/**/*_test.go` | Run via `make test` |
| Ledger invariant tests | `internal/ledger/` | Property-based; assert balance integrity under reordering/duplication |
| PSP contract tests | `test/contract/` | Run against each PSP's sandbox; require `.env` sandbox credentials |
| End-to-end tests | `test/e2e/` | Full flow: create intent → simulate webhook → assert ledger + merchant webhook dispatched |
| Chaos/failover tests | `test/e2e/failover_test.go` | Simulate PSP timeout/5xx and assert routing failover behaves correctly with no double-charge |

CI runs unit + ledger invariant tests on every PR. Contract and e2e tests run on merge to `main` and nightly.

---

## Observability

- Distributed tracing via OpenTelemetry — every `PaymentIntent` carries a trace ID through orchestration, connector calls, webhook processing, and ledger posting.
- Metrics exported to Prometheus: per-PSP success rate, time-to-confirmation, webhook lag, reconciliation exceptions, connector circuit-breaker state.
- Dashboards in Grafana (`deploy/grafana/dashboards/`).
- Alerting (PagerDuty/Slack) on: signature-failure spikes, reconciliation exceptions, circuit breaker opens, **any ledger imbalance** (page-immediately severity).

---

## Security

- No raw card data ever touches Zia's backend (PSP-hosted tokenization only — Stripe Elements, Paystack inline, PayPal SDK). Keeps PCI DSS scope at SAQ A.
- Secrets managed via Vault/KMS in staging and production; never committed, never logged.
- Per-merchant tenant isolation enforced at the query layer and via Postgres Row-Level Security.
- Full audit trail on every state transition and manual ops action.

Full threat model and control list: `ARCHITECTURE.md §10`.

If you discover a security issue, **do not open a public GitHub issue** — email `security@Zia.dev` (or your configured security contact) directly.

---

## Roadmap

- [x] V1 — Core switch, 6 PSPs, server-to-server API, modular monolith
- [ ] V2 — Embeddable JS checkout widget (built on the existing Checkout Session API)
- [ ] V2 — Hosted checkout page fallback for merchants without frontend dev resources
- [ ] V3 — Success-rate/cost-weighted smart routing
- [ ] V3 — Additional rails (Airtel Money direct, additional African card processors)
- [ ] V3 — Merchant self-serve dashboard
- [ ] V4 — Extract Connector Layer / Ledger Service into independent services; multi-region

---

## Contributing

1. Fork and branch off `main`.
2. Run `make lint test` before opening a PR.
3. Any change touching `internal/ledger/` or `internal/domain/` requires a second reviewer.
4. New PSP connectors must implement the full `connector.Connector` interface and include contract tests against the provider's sandbox before merge.
5. Follow conventional commits (`feat:`, `fix:`, `chore:`, etc.) — used to generate the changelog.

---

## License

Proprietary — All rights reserved, © Zia.

---

## Support

- Engineering questions: `#Zia-eng` (internal)
- Architecture deep-dive: see [`ARCHITECTURE.md`](./ARCHITECTURE.md)
- Security issues: `security@Zia.dev`