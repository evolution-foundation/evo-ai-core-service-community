-- Reverse of 000016: drop `tenant_id` from the eight community tables.
-- Use IF EXISTS so a partial-up state still rolls back cleanly.

ALTER TABLE evo_core_agents DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE evo_core_agent_integrations DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE evo_core_api_keys DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE evo_core_custom_mcp_servers DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE evo_core_custom_tools DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE evo_core_folders DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE evo_core_folder_shares DROP COLUMN IF EXISTS tenant_id;
ALTER TABLE evo_core_mcp_servers DROP COLUMN IF EXISTS tenant_id;
