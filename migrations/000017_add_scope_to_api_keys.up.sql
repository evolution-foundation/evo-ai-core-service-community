-- Credentials resolve through an ordered scope chain (installation -> account),
-- where the most specific link wins. Existing rows are account-level by default;
-- promoting values between scopes is a separate data migration.
ALTER TABLE evo_core_api_keys
ADD COLUMN IF NOT EXISTS scope VARCHAR(32) NOT NULL DEFAULT 'account';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'evo_core_api_keys_scope_check'
    ) THEN
        ALTER TABLE evo_core_api_keys
        ADD CONSTRAINT evo_core_api_keys_scope_check
        CHECK (scope IN ('installation', 'account'));
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE tablename = 'evo_core_api_keys'
        AND indexname = 'idx_evo_core_api_keys_scope_active'
    ) THEN
        CREATE INDEX idx_evo_core_api_keys_scope_active
        ON evo_core_api_keys (scope, is_active);
    END IF;
END
$$;
