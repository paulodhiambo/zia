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
    key_prefix  TEXT NOT NULL,
    environment TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payment_intents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id     UUID NOT NULL REFERENCES merchants(id),
    amount_minor    BIGINT NOT NULL,
    currency        TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'created',
    method          TEXT NOT NULL DEFAULT 'mpesa_stk',
    customer_ref    TEXT,
    customer_phone  TEXT,
    customer_email  TEXT,
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
    WHERE psp_reference IS NOT NULL;

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
    attempt_id        UUID REFERENCES attempts(id),
    amount_minor      BIGINT NOT NULL,
    currency          TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending',
    reason            TEXT,
    psp_reference     TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE payouts (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id  UUID NOT NULL REFERENCES merchants(id),
    amount_minor BIGINT NOT NULL,
    currency     TEXT NOT NULL,
    rail         TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending',
    psp_reference TEXT,
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

CREATE TABLE pesalink_recipients (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id      UUID NOT NULL REFERENCES merchants(id),
    currency         TEXT NOT NULL,
    account_hash     TEXT NOT NULL,
    pesalink_acct_id TEXT NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (merchant_id, currency, account_hash)
);
