ALTER TABLE payment_intents RENAME COLUMN amount TO amount_minor;
ALTER TABLE ledger_entries RENAME COLUMN amount TO amount_minor;
ALTER TABLE refunds RENAME COLUMN amount TO amount_minor;
ALTER TABLE payouts RENAME COLUMN amount TO amount_minor;
