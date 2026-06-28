DROP INDEX IF EXISTS idx_merchants_code;
ALTER TABLE merchants DROP COLUMN IF EXISTS code;
