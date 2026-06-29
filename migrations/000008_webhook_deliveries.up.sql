CREATE TABLE webhook_deliveries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    webhook_event_id UUID REFERENCES webhook_events(id),
    endpoint_id     UUID NOT NULL REFERENCES webhook_endpoints(id),
    url             TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    request_headers JSONB,
    request_body    BYTEA,
    response_status INT,
    response_body   BYTEA,
    duration_ms     INT,
    attempt         INT NOT NULL DEFAULT 1,
    max_attempts    INT NOT NULL DEFAULT 3,
    next_retry_at   TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_wd_event ON webhook_deliveries(webhook_event_id);
CREATE INDEX idx_wd_retry ON webhook_deliveries(next_retry_at) WHERE status = 'failed' AND next_retry_at IS NOT NULL;
