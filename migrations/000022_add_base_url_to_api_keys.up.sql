-- The endpoint a credential talks to, alongside the credential itself.
--
-- The screen has always rendered and sent `base_url` (story 1.1), but there
-- was nowhere to put it: the value was silently discarded on every save, and
-- with it went 1.5 AC1 ("OPENAI_API_URL preserved next to the credential") and
-- the 1.3 task "a resolved credential carrying its own URL wins".
--
-- It belongs on the credential, not in installation config: an OpenAI-compatible
-- provider (Azure, a local gateway, a proxy) is defined by the PAIR key+endpoint,
-- so two credentials on the same installation can legitimately point at
-- different hosts. NULL means "the provider default", which is what every
-- credential registered before this column meant.
ALTER TABLE evo_core_api_keys
ADD COLUMN IF NOT EXISTS base_url VARCHAR(512);
