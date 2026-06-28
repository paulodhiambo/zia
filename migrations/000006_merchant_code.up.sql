ALTER TABLE merchants ADD COLUMN code TEXT NOT NULL DEFAULT '';

-- Backfill existing merchants with unique codes
DO $$
DECLARE
    m RECORD;
    c TEXT;
BEGIN
    FOR m IN SELECT id FROM merchants WHERE code = '' LOOP
        c := 'M-' || upper(substr(md5(random()::text), 1, 6));
        WHILE EXISTS (SELECT 1 FROM merchants WHERE code = c) LOOP
            c := 'M-' || upper(substr(md5(random()::text), 1, 6));
        END LOOP;
        UPDATE merchants SET code = c WHERE id = m.id;
    END LOOP;
END $$;

CREATE UNIQUE INDEX idx_merchants_code ON merchants(code);
