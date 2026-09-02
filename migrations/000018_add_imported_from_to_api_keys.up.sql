-- Records which legacy source a credential was imported from, so the migration
-- is idempotent without using the name as its key: a human may rename, disable
-- or replace an imported credential, and a re-run must respect that.
ALTER TABLE evo_core_api_keys
ADD COLUMN IF NOT EXISTS imported_from VARCHAR(64);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE tablename = 'evo_core_api_keys'
        AND indexname = 'idx_evo_core_api_keys_imported_from'
    ) THEN
        CREATE UNIQUE INDEX idx_evo_core_api_keys_imported_from
        ON evo_core_api_keys (imported_from)
        WHERE imported_from IS NOT NULL;
    END IF;
END
$$;
