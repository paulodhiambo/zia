ALTER TABLE payment_intents RENAME COLUMN amount_minor TO amount;
ALTER TABLE ledger_entries RENAME COLUMN amount_minor TO amount;
ALTER TABLE refunds RENAME COLUMN amount_minor TO amount;
ALTER TABLE payouts RENAME COLUMN amount_minor TO amount;
