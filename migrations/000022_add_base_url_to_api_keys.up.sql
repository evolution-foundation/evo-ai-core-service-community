-- The endpoint a credential talks to, stored next to the credential.
---- It belongs here rather than in installation config: an OpenAI-compatible
-- provider is defined by the pair key+endpoint, so two credentials on the same
-- installation can legitimately point at different hosts. NULL means "the
-- provider default".
ALTER TABLE evo_core_api_keys
ADD COLUMN IF NOT EXISTS base_url VARCHAR(512);
