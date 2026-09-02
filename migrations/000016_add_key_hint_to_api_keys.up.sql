-- Store a masked hint (last 4 characters of the plaintext key) so the UI can
-- render a mask without the API ever returning the key itself.
ALTER TABLE evo_core_api_keys
ADD COLUMN IF NOT EXISTS key_hint VARCHAR(8) NOT NULL DEFAULT '';
