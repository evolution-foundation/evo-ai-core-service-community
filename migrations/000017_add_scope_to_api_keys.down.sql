DROP INDEX IF EXISTS idx_evo_core_api_keys_scope_active;

ALTER TABLE evo_core_api_keys
DROP CONSTRAINT IF EXISTS evo_core_api_keys_scope_check;

ALTER TABLE evo_core_api_keys
DROP COLUMN IF EXISTS scope;
