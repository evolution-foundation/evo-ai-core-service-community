-- Vault references for tool and MCP secrets (EVO-2250, story 2.4).
--
-- It is a MAP (header or env var name -> credential id), not a scalar column:
-- one credential equals one secret, so a tool with two auth headers references
-- TWO credentials. A scalar column could not say WHICH header it replaces.
--
-- The inline `headers` stay untouched: they are the fallback until story 2.7
-- retires them, so nothing breaks before the 2.6 migration runs.
ALTER TABLE evo_core_custom_tools
ADD COLUMN IF NOT EXISTS credential_refs JSONB NOT NULL DEFAULT '{}';

ALTER TABLE evo_core_custom_mcp_servers
ADD COLUMN IF NOT EXISTS credential_refs JSONB NOT NULL DEFAULT '{}';
