CREATE TABLE notification_preferences (
    merchant_id UUID PRIMARY KEY REFERENCES merchants(id),
    preferences JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
