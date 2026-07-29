package repository

import (
	"context"

	"evo-ai-core-service/pkg/integration_credential/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// oauthConnectionRepository reads the OWNER store (evo_core_agent_integrations)
// and persists the vault's reference rows.
//
// The read pulls metadata only: the id of the integration row, the agent it
// belongs to, the provider and the expiry. `access_token` is deliberately NOT
// selected, so a token cannot reach the vault even by accident.
type oauthConnectionRepository struct {
	db *gorm.DB
}

func NewOAuthConnectionRepository(db *gorm.DB) *oauthConnectionRepository { //nolint:revive // adapter consumed through the service interfaces
	return &oauthConnectionRepository{db: db}
}

// LiveConnections returns every agent integration that currently holds an
// access token. The join carries the agent name for the label and the agent id,
// which the disconnect path needs.
func (r *oauthConnectionRepository) LiveConnections(ctx context.Context) ([]model.OAuthConnection, error) {
	var rows []struct {
		IntegrationID string
		AgentID       string
		AgentName     string
		Provider      string
		ExpiresAt     string
	}

	err := r.db.WithContext(ctx).
		Table("evo_core_agent_integrations AS ai").
		Select(`ai.id AS integration_id,
			ai.agent_id AS agent_id,
			COALESCE(a.name, '') AS agent_name,
			ai.provider AS provider,
			COALESCE(ai.config ->> 'expires_at', '') AS expires_at`).
		Joins("LEFT JOIN evo_core_agents AS a ON a.id = ai.agent_id").
		Where("ai.config ->> 'access_token' IS NOT NULL AND ai.config ->> 'access_token' <> ''").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	connections := make([]model.OAuthConnection, 0, len(rows))
	for _, row := range rows {
		connections = append(connections, model.OAuthConnection{
			IntegrationID: row.IntegrationID,
			AgentID:       row.AgentID,
			AgentName:     row.AgentName,
			Provider:      row.Provider,
			ExpiresAt:     row.ExpiresAt,
		})
	}

	return connections, nil
}

func (r *oauthConnectionRepository) ListOAuthRows(ctx context.Context) ([]model.IntegrationCredential, error) {
	var rows []model.IntegrationCredential

	err := r.db.WithContext(ctx).
		Where("kind = ?", model.KindOAuth).
		Find(&rows).Error

	return rows, err
}

// UpsertOAuthRow writes the reference by its natural key. A returning
// connection reactivates its existing row instead of creating a second one.
func (r *oauthConnectionRepository) UpsertOAuthRow(ctx context.Context, row model.IntegrationCredential) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "owner_store"}, {Name: "owner_ref"}},
			DoUpdates: clause.AssignmentColumns([]string{"name", "provider", "is_active", "updated_at"}),
		}).
		Create(&row).Error
}

func (r *oauthConnectionRepository) DeactivateOAuthRow(ctx context.Context, ownerRef string) error {
	return r.db.WithContext(ctx).
		Model(&model.IntegrationCredential{}).
		Where("kind = ? AND owner_store = ? AND owner_ref = ?", model.KindOAuth, model.OwnerStoreAgentIntegration, ownerRef).
		Update("is_active", false).Error
}
