package repository

import (
	"context"

	"evo-ai-core-service/pkg/integration_credential/model"

	"gorm.io/gorm"
)

// migrationStateRepository counts what is left to migrate, per consumer.
//
// Each query is scoped to secrets that have NO vault reference replacing them:
// that is what "still inline" means, and it is why an installation that
// migrated reports zero even though the inline columns were deliberately not
// deleted (story 2.6 keeps them for the fallback).
type migrationStateRepository struct {
	db *gorm.DB
}

func NewMigrationStateRepository(db *gorm.DB) *migrationStateRepository { //nolint:revive // adapter consumed through the service interface
	return &migrationStateRepository{db: db}
}

func (r *migrationStateRepository) ImportedCredentials(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("evo_core_integration_credentials").
		Where("imported_from IS NOT NULL").
		Count(&count).Error

	return count, err
}

func (r *migrationStateRepository) PendingInlineSecrets(ctx context.Context, consumer string) (int64, error) {
	switch consumer {
	case model.ConsumerCustomTools:
		return r.pendingHeaders(ctx, "evo_core_custom_tools")
	case model.ConsumerCustomMcpServers:
		return r.pendingHeaders(ctx, "evo_core_custom_mcp_servers")
	case model.ConsumerKnowledgeNexus:
		return r.pendingIntegrationSecret(ctx, "knowledge_nexus", "nexus_api_key")
	case model.ConsumerExternalAgents:
		return r.pendingExternalAgents(ctx)
	case model.ConsumerAgentBots:
		// agent_bots lives in the CRM schema, which this service does not own:
		// the CRM guard (Ai::IntegrationMigrationState) is the one that can
		// actually answer for it. Reporting zero here would read as "retired"
		// and let story 2.7 remove the bot's inline fallback on an installation
		// that never migrated — the exact fail-open the guard forbids. A
		// constant pending keeps the consumer NOT retired until a CRM-backed
		// signal exists (adversarial review, 2026-07-29: the previous return 0
		// contradicted this very comment).
		return 1, nil
	default:
		return 0, nil
	}
}

// A header is still inline when it looks like authentication and no
// credential_refs entry replaces it.
func (r *migrationStateRepository) pendingHeaders(ctx context.Context, table string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table(table).
		Where("is_active = ?", true).
		Where(`EXISTS (
			SELECT 1 FROM jsonb_each_text(headers::jsonb) AS h(key, value)
			WHERE lower(h.key) IN ('authorization','x-api-key','api-key','apikey','x-auth-token')
			AND h.value <> ''
			AND NOT (credential_refs ? h.key)
		)`).
		Count(&count).Error

	return count, err
}

func (r *migrationStateRepository) pendingIntegrationSecret(ctx context.Context, provider, field string) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("evo_core_agent_integrations").
		Where("provider = ?", provider).
		Where("config ->> ? <> ''", field).
		Where("config ->> 'credential_id' IS NULL").
		Count(&count).Error

	return count, err
}

func (r *migrationStateRepository) pendingExternalAgents(ctx context.Context) (int64, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("evo_core_agent_integrations").
		Where("provider IN ?", []string{"dify", "flowise", "n8n", "openai", "elevenlabs"}).
		Where("config ->> 'credential_id' IS NULL").
		Where("(config ->> 'apiKey' <> '' OR config ->> 'basicAuthPass' <> '')").
		Count(&count).Error

	return count, err
}
