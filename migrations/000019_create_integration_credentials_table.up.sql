-- The integration credential vault: the secret a tool or integration needs to
-- act (Dify key, n8n basic auth, MCP header, Knowledge Nexus key).
---- `kind` keeps the vault from becoming a refresh subsystem: a `static` row owns
-- its encrypted value, an `oauth` row owns nothing and points at the store that
-- already refreshes the token.
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
    -- Enforced by the database, not by convention: the whole point of the oauth
    -- kind is that no token value ever lands here.
    CONSTRAINT evo_core_integration_credentials_kind_content_check
        CHECK (
            (kind = 'static' AND value IS NOT NULL AND owner_store IS NULL AND owner_ref IS NULL)
            OR
            (kind = 'oauth' AND value IS NULL AND owner_store IS NOT NULL AND owner_ref IS NOT NULL)
        ),
    -- Per scope, NEVER on name alone: the sibling tables do that, and in the
    -- enterprise build two tenants naming a credential "Producao" collide.
    -- Adding tenant_id here is then a gem migration, not a live constraint drop.
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
