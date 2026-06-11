package model

import (
	"encoding/json"
	"evo-ai-core-service/internal/infra/postgres"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type AgentIntegration struct {
	ID        uuid.UUID      `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	TenantID  uuid.UUID      `json:"-" gorm:"column:tenant_id;type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'"`
	AgentID   uuid.UUID      `json:"-" gorm:"<-:create;not null;type:uuid"`
	Provider  string         `json:"-" gorm:"not null;type:varchar(100)"`
	Config    datatypes.JSON `json:"-" gorm:"type:jsonb;default:'{}'"`
	CreatedAt time.Time      `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt time.Time      `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (AgentIntegration) TableName() string {
	return "evo_core_agent_integrations"
}

type AgentIntegrationRequest struct {
	Provider string                 `json:"provider" binding:"required"`
	Config   map[string]interface{} `json:"config" binding:"required"`
}

type AgentIntegrationResponse struct {
	ID        uuid.UUID              `json:"id"`
	AgentID   uuid.UUID              `json:"agent_id"`
	Provider  string                 `json:"provider"`
	Config    map[string]interface{} `json:"config"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// sanitizeConfig removes ALL sensitive fields from integration config before returning to frontend.
// Security: Frontend should NEVER receive access_token, client_id, or any credentials.
// Discovery of tools should be done via backend endpoints that use stored credentials.
func sanitizeConfig(config map[string]interface{}) map[string]interface{} {
	if config == nil {
		return config
	}

	// Create a copy to avoid modifying the original
	sanitized := make(map[string]interface{})
	for k, v := range config {
		sanitized[k] = v
	}

	// List of sensitive fields to remove (including access_token and client_id)
	sensitiveFields := []string{
		"access_token",
		"client_id",
		"client_secret",
		"refresh_token",
		"pkce_verifiers",
		"token", // Google Calendar token
		"code_verifier",
	}

	// Remove sensitive fields
	for _, field := range sensitiveFields {
		delete(sanitized, field)
	}

	// Remove any token-like values (REST API keys: sk_, rk_, pk_)
	for key, value := range sanitized {
		if strValue, ok := value.(string); ok {
			if len(strValue) >= 3 && (strValue[:3] == "sk_" || strValue[:3] == "rk_" || strValue[:3] == "pk_") {
				delete(sanitized, key)
			}
		}
	}

	return sanitized
}

func (a *AgentIntegration) ToResponse() *AgentIntegrationResponse {
	// Unmarshal Config from datatypes.JSON to map
	var configMap map[string]interface{}
	if len(a.Config) > 0 {
		_ = json.Unmarshal(a.Config, &configMap)
	}
	if configMap == nil {
		configMap = make(map[string]interface{})
	}

	// Sanitize config to remove sensitive fields before returning
	sanitizedConfig := sanitizeConfig(configMap)

	return &AgentIntegrationResponse{
		ID:        a.ID,
		AgentID:   a.AgentID,
		Provider:  a.Provider,
		Config:    sanitizedConfig,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

var AgentIntegrationErrors = []postgres.CustomErrorMessage{
	{
		Code:    "ERR_RECORD_NOT_FOUND",
		Message: "Integration not found",
	},
}
