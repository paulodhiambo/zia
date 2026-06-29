# Zia

**A unified payment aggregator/switch for Africa-first commerce.**

> "Zia" — Swahili for "cross over." Zia is the bridge merchants use to move money across payment rails without building and maintaining six separate integrations.

[![Go Version](https://img.shields.io/badge/go-1.26-00ADD8)]()
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

**V1 (live):**

| Provider | Role | Region | Flow |
|---|---|---|---|
| **M-Pesa** (Daraja) | Collection (primary, KE) + B2C refunds/payouts | Kenya | STK Push, async via callback |
| **Paystack** | Collection (cards + mobile money) | Africa-wide | Hosted/inline checkout, webhook-confirmed |

**Roadmap (V2+):**

| Provider | Planned role | Notes |
|---|---|---|
| **Stripe** | Cards/wallets, global | Stripe.js/Elements — no raw PAN on our servers |
| **PayPal** | Cards/wallet, international | Orders API v2, redirect/approve/capture |


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
| Language | Go 1.26 |
| HTTP | `chi` router |
| Database | PostgreSQL 15+ |
| Cache / idempotency / locks | Redis 7+ |
| Event bus | NATS JetStream |
| Secrets | `.env` (local) / Helm `Secret` (production) |
| Background jobs | NATS JetStream consumer (`cmd/worker`) + `cmd/cron` runner |
| Observability | OpenTelemetry → **OpenObserve** (traces, metrics, logs via OTLP) |
| Containerization | Docker + Docker Compose (local) |
| Deployment | Helm + **ArgoCD** (GitOps) |

---

## Project Structure

```
zia/
├── cmd/
│   ├── api/          # HTTP API server entrypoint
│   ├── worker/       # NATS JetStream consumer (webhook processing, notifications)
│   └── cron/         # scheduled jobs (reconciliation, settlement, M-Pesa token refresh)
├── internal/
│   ├── api/          # HTTP handlers: portal, merchant, payment_intent, checkout, webhook
│   ├── authn/        # API key authentication middleware
│   ├── domain/       # PaymentIntent, Attempt, LedgerEntry, Merchant, User, etc.
│   ├── orchestrator/ # the Switch — state machine, no PSP-specific code
│   ├── routing/      # rule-based routing engine + circuit breaker
│   ├── idempotency/
│   ├── ledger/
│   ├── reconciliation/
│   ├── settlement/
│   ├── webhook/
│   ├── notification/
│   ├── risk/
│   ├── repository/   # DB access layer (pgx/v5)
│   ├── service/
│   ├── telemetry/
│   └── connector/
│       ├── connector.go   # Connector interface + shared types
│       ├── registry.go
│       ├── mpesa/
│       └── paystack/
├── pkg/
│   ├── httpsign/     # HMAC signing helpers
│   ├── moneyutil/    # minor-unit arithmetic, no floats
│   └── phoneutil/    # E.164 phone normalization
├── migrations/       # golang-migrate SQL files
├── helm/             # Helm chart (ConfigMap + Secret split, HPA, PDB, ingress)
├── argocd/           # ArgoCD Application manifests
├── api-tests/        # Postman collection
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── .env.example
```

---

## Prerequisites

- Go 1.26 or later
- Docker & Docker Compose
- PostgreSQL 15+ (provided via Docker Compose for local dev)
- Redis 7+ (provided via Docker Compose for local dev)
- `make`
- Sandbox/developer accounts for the PSPs you intend to test:
    - [Safaricom Daraja](https://developer.safaricom.co.ke/) — M-Pesa (required for V1)
    - [Paystack](https://dashboard.paystack.com/#/signup) — test mode keys (required for V1)
    - [Stripe](https://dashboard.stripe.com/register) / [PayPal Developer](https://developer.paypal.com/) — V2+ only

---

## Getting Started

```bash
# 1. Clone the repo
git clone https://github.com/paulodhiambo/zia.git
cd zia

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
PORT=8080
APP_ENV=development
DATABASE_URL=postgres://zia:zia@localhost:5432/zia?sslmode=disable
REDIS_URL=redis://localhost:6379/0
NATS_URL=nats://localhost:4222
HMAC_SIGNING_SECRET=dev-secret-do-not-use-in-production

# Platform fees
PLATFORM_FEE_PERCENT=2       # percentage of transaction amount
PLATFORM_FEE_MIN=100         # minimum fee in minor units (e.g. 100 = 1.00 KES)

# Observability (OpenObserve)
OO_ENDPOINT=http://localhost:5080
OO_EMAIL=admin@zia.dev
OO_PASSWORD=
OO_SERVICE_NAME=zia-api
OO_ENVIRONMENT=development
OO_SAMPLE_RATE=1.0

# M-Pesa (Daraja)
MPESA_CONSUMER_KEY=
MPESA_CONSUMER_SECRET=
MPESA_SHORTCODE=
MPESA_PASSKEY=
MPESA_CALLBACK_BASE_URL=https://your-ngrok-or-domain.example.com
MPESA_ALLOWED_IPS=           # comma-separated Safaricom egress IPs for IP allowlisting
MPESA_B2C_INITIATOR_NAME=
MPESA_B2C_SECURITY_CREDENTIAL=

# Paystack
PAYSTACK_SECRET_KEY=
PAYSTACK_WEBHOOK_SECRET=

# SMS (OpenSMS — for portal OTP delivery)
SMS_API_TOKEN=
SMS_SENDER_ID=
```

> **Never commit `.env` or real credentials.** In production, secrets are injected via Helm `Secret` (or a pre-existing secret managed by Sealed Secrets / External Secrets Operator) — see `ARCHITECTURE.md §10`.

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

All requests use the `RequestEnvelope` wrapper (`messageId` is used as the idempotency key):

```bash
curl -X POST https://api.zia.dev/v1/payment_intents \
  -H "Authorization: Bearer <YOUR_SECRET_KEY>" \
  -H "Content-Type: application/json" \
  -d '{
    "messageId": "'"$(uuidgen)"'",
    "conversationId": "session-001",
    "primaryData": {
      "amountMinor": 50000,
      "currency": "KES",
      "method": "mpesa_stk",
      "customerPhone": "254712345678",
      "customerRef": "order_8841"
    }
  }'
```

```json
{
  "id": "pi_01HZX...",
  "status": "requires_action",
  "amountMinor": 50000,
  "currency": "KES",
  "method": "mpesa_stk",
  "createdAt": "2026-06-29T08:00:00Z",
  "updatedAt": "2026-06-29T08:00:00Z"
}
```

The customer receives the M-Pesa PIN prompt on their phone. Zia updates the `PaymentIntent` to `succeeded` or `failed` asynchronously once Safaricom's callback (or the fallback reconciliation poll) confirms the outcome — your integration should rely on the **outbound webhook** or **polling `GET /v1/payment_intents/:id`**, not a synchronous response.

### Check status

```bash
curl https://api.zia.dev/v1/payment_intents/pi_01HZX... \
  -H "Authorization: Bearer <YOUR_SECRET_KEY>"
```

### Issue a refund

```bash
curl -X POST https://api.zia.dev/v1/payment_intents/pi_01HZX.../refunds \
  -H "Authorization: Bearer <YOUR_SECRET_KEY>" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{ "amount_minor": 50000 }'
```

### Create a checkout session (widget-ready, future-facing)

```bash
curl -X POST https://api.zia.dev/v1/checkout_sessions \
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
- Traces, metrics, and logs shipped via OTLP to **OpenObserve** (configured via `OO_*` env vars).
- Key metrics: per-PSP success rate, P50/P95/P99 time-to-confirmation, webhook processing lag, reconciliation exception count, circuit-breaker state.
- Alerting on: signature-failure spikes, reconciliation exceptions, circuit breaker opens, **any ledger imbalance** (page-immediately severity).

---

## Security

- No raw card data ever touches Zia's backend (PSP-hosted tokenization only — Stripe Elements, Paystack inline, PayPal SDK). Keeps PCI DSS scope at SAQ A.
- Secrets injected via Helm `Secret` in production (supports Sealed Secrets / External Secrets Operator); never committed, never logged.
- Per-merchant tenant isolation enforced at the query layer and via Postgres Row-Level Security.
- Full audit trail on every state transition and manual ops action.

Full threat model and control list: `ARCHITECTURE.md §10`.

If you discover a security issue, **do not open a public GitHub issue** — email `security@zia.dev` (or your configured security contact) directly.

---

## Roadmap

- [x] V1 — M-Pesa (STK Push + B2C) + Paystack (cards/mobile money), server-to-server API, merchant portal, modular monolith
- [ ] V2 — Stripe (global cards/wallets) + PayPal (Orders API v2)
- [ ] V2 — Embeddable JS checkout widget (built on the existing Checkout Session API)
- [ ] V2 — Hosted checkout page fallback for merchants without frontend dev resources
- [ ] V3 — Success-rate/cost-weighted smart routing (same `Router` interface, no API change)
- [ ] V3 — Airtel Money and additional African card processors
- [ ] V3 — KYC/AML merchant onboarding module
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
- Security issues: `security@zia.dev`