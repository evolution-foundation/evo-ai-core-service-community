-- The natural key of an oauth reference row is the store that owns the token
-- plus the row in it. The sync (story 2.5) upserts on this key, so a connection
-- that disappears and comes back reactivates its row instead of producing a
-- second one.
--
-- Partial index: only oauth rows have an owner, and static rows keep both
-- columns NULL by the coherence CHECK of migration 000019.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE tablename = 'evo_core_integration_credentials'
        AND indexname = 'idx_evo_core_integration_credentials_owner_unique'
    ) THEN
        CREATE UNIQUE INDEX idx_evo_core_integration_credentials_owner_unique
        ON evo_core_integration_credentials (owner_store, owner_ref)
        WHERE owner_store IS NOT NULL AND owner_ref IS NOT NULL;
    END IF;
END
$$;
