-- The integration credential vault: the secret a tool or integration needs to
-- act (Dify key, n8n basic auth, MCP header, Knowledge Nexus key). Deliberately
-- NOT evo_core_api_keys, which holds model-provider keys as a simple pair.
--
-- `kind` is the discriminator that keeps the vault from becoming a refresh
-- subsystem: a `static` row owns its (encrypted) value, while an `oauth` row
-- owns nothing and points at the store that already refreshes the token.
-- Story 2.5 opens the oauth path; 2.1 only stores static secrets.
CREATE TABLE IF NOT EXISTS evo_core_integration_credentials (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    provider VARCHAR(100) NOT NULL,
    kind VARCHAR(16) NOT NULL DEFAULT 'static',
    value TEXT,
    value_format VARCHAR(16) NOT NULL DEFAULT 'scalar',
    value_hint VARCHAR(8) NOT NULL DEFAULT '',
    scope VARCHAR(32) NOT NULL DEFAULT 'account',
    owner_store VARCHAR(64),
    owner_ref VARCHAR(128),
    imported_from VARCHAR(128),
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT evo_core_integration_credentials_kind_check
        CHECK (kind IN ('static', 'oauth')),
    CONSTRAINT evo_core_integration_credentials_value_format_check
        CHECK (value_format IN ('scalar', 'composite')),
    CONSTRAINT evo_core_integration_credentials_scope_check
        CHECK (scope IN ('installation', 'account')),
    -- Coherence between kind and content, enforced by the database rather than
    -- by convention: a convention breaks in a distracted pull request, and the
    -- whole point of the oauth kind is that no token value ever lands here.
    CONSTRAINT evo_core_integration_credentials_kind_content_check
        CHECK (
            (kind = 'static' AND value IS NOT NULL AND owner_store IS NULL AND owner_ref IS NULL)
            OR
            (kind = 'oauth' AND value IS NULL AND owner_store IS NOT NULL AND owner_ref IS NOT NULL)
        ),
    -- Uniqueness is per scope, NEVER on name alone. The three sibling tables
    -- (custom_tools, custom_mcp_servers, mcp_servers) unique on name alone, and
    -- in the enterprise build two tenants naming a credential "Producao"
    -- collide in the database. Adding tenant_id to this index is then a gem
    -- migration, with no need to drop a live unique constraint.
    CONSTRAINT evo_core_integration_credentials_scope_name_unique
        UNIQUE (scope, name)
);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE tablename = 'evo_core_integration_credentials'
        AND indexname = 'idx_evo_core_integration_credentials_scope_active'
    ) THEN
        CREATE INDEX idx_evo_core_integration_credentials_scope_active
        ON evo_core_integration_credentials (scope, is_active);
    END IF;

    IF NOT EXISTS (
        SELECT 1
        FROM pg_indexes
        WHERE tablename = 'evo_core_integration_credentials'
        AND indexname = 'idx_evo_core_integration_credentials_kind_provider'
    ) THEN
        CREATE INDEX idx_evo_core_integration_credentials_kind_provider
        ON evo_core_integration_credentials (kind, provider);
    END IF;
END
$$;
