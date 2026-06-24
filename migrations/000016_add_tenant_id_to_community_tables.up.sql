-- Add `tenant_id` to the eight community tables whose Go models declare a
-- TenantID field. Without this column, every INSERT issued by the community
-- build fails with: column "tenant_id" of relation "..." does not exist.
--
-- Pre-existing bug surfaced while validating the EVO-1790 custom-tool test
-- endpoint fix. Architectural question — "should community keep `tenant_id`
-- permanently or strip it (treating multi-tenancy as enterprise-only)?" —
-- is left for the backend team to triage separately. This migration is
-- purely additive and fully reversible by the .down.sql counterpart.
--
-- Default value matches the GORM struct tag in every affected model:
--   `gorm:"column:tenant_id;type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'"`

ALTER TABLE evo_core_agents
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE evo_core_agent_integrations
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE evo_core_api_keys
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE evo_core_custom_mcp_servers
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE evo_core_custom_tools
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE evo_core_folders
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE evo_core_folder_shares
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';

ALTER TABLE evo_core_mcp_servers
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000';
