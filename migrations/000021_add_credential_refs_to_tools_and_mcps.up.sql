-- Vault references for tool and MCP secrets.
---- A MAP (header or env var name -> credential id), not a scalar: one credential
-- is one secret, so a tool with two auth headers references two, and a scalar
-- could not say which header it replaces.
---- The inline `headers` stay as the fallback until retirement.
ALTER TABLE evo_core_custom_tools
ADD COLUMN IF NOT EXISTS credential_refs JSONB NOT NULL DEFAULT '{}';

ALTER TABLE evo_core_custom_mcp_servers
ADD COLUMN IF NOT EXISTS credential_refs JSONB NOT NULL DEFAULT '{}';
