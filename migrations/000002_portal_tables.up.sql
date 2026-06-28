CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id),
    name       TEXT NOT NULL,
    email      TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    phone      TEXT NOT NULL DEFAULT '',
    role       TEXT NOT NULL DEFAULT 'Owner',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_users_email ON users(merchant_id, email);

CREATE TABLE sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id),
    token      TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_sessions_token ON sessions(token);

CREATE TABLE customers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id  UUID NOT NULL REFERENCES merchants(id),
    name         TEXT NOT NULL,
    company      TEXT NOT NULL,
    email        TEXT NOT NULL,
    phone        TEXT NOT NULL DEFAULT '',
    location     TEXT NOT NULL DEFAULT '',
    volume_minor BIGINT NOT NULL DEFAULT 0,
    ltv_minor    BIGINT NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'Active',
    payment_method TEXT NOT NULL DEFAULT '',
    joined_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_customers_merchant ON customers(merchant_id);

CREATE TABLE team_members (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id),
    user_id     UUID NOT NULL REFERENCES users(id),
    name        TEXT NOT NULL,
    email       TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'Read-only',
    last_active TIMESTAMPTZ,
    initials    TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_team_members_merchant ON team_members(merchant_id);

CREATE TABLE team_invitations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id),
    email       TEXT NOT NULL,
    role        TEXT NOT NULL DEFAULT 'Read-only',
    token       TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_invitations_merchant ON team_invitations(merchant_id);

CREATE TABLE webhook_endpoints (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id),
    url         TEXT NOT NULL,
    events      INT NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'Healthy',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhook_endpoints_merchant ON webhook_endpoints(merchant_id);

CREATE TABLE notifications (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    merchant_id UUID NOT NULL REFERENCES merchants(id),
    tone       TEXT NOT NULL DEFAULT 'neutral',
    title      TEXT NOT NULL,
    body       TEXT NOT NULL,
    category   TEXT NOT NULL DEFAULT 'Product',
    unread     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_notifications_merchant ON notifications(merchant_id);

ALTER TABLE api_keys ADD COLUMN name TEXT NOT NULL DEFAULT '';
