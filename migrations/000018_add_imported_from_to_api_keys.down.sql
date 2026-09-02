DROP INDEX IF EXISTS idx_evo_core_api_keys_imported_from;

ALTER TABLE evo_core_api_keys
DROP COLUMN IF EXISTS imported_from;
