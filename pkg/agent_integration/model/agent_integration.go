package model

import (
	"encoding/json"
	"evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/evoextensions/tenantfield"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type AgentIntegration struct {
	tenantfield.TenantField

	ID        uuid.UUID      `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
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

// sensitiveFieldNames are the config keys that must never reach a client.
// // The same list drives MergePreservedSecrets: a field that stops being returned
// must also stop being overwritten by a save that never carried it.
var sensitiveFieldNames = []string{
	"access_token",
	"client_id",
	"client_secret",
	"refresh_token",
	"pkce_verifiers",
	"token", // Google Calendar token
	"code_verifier",
	// Platform credentials of external agents and native tools.
	"apiKey",
	"api_key",
	"basicAuthUser", // half of a basic auth pair is still half a credential
	"basicAuthPass",
	"nexus_api_key",
}

// CredentialIDFrom reports the vault reference carried by a config, if any.
// Its absence is the signal to fall back to the inline value, which is what
// keeps this story from breaking installations that have not migrated.
func CredentialIDFrom(config map[string]interface{}) (string, bool) {
	if config == nil {
		return "", false
	}

	value, ok := config["credential_id"].(string)
	if !ok || value == "" {
		return "", false
	}

	return value, true
}

// MergePreservedSecrets carries stored secrets over into an incoming config
// that omits them. The upsert replaces `config` wholesale, and sanitizeConfig
// means a save arrives without the secrets it never received.
func MergePreservedSecrets(incoming, stored map[string]interface{}) map[string]interface{} {
	merged := make(map[string]interface{}, len(incoming))
	for key, value := range incoming {
		merged[key] = value
	}

	if stored == nil {
		return merged
	}

	for _, field := range sensitiveFieldNames {
		storedValue, exists := stored[field]
		if !exists {
			continue
		}

		incomingValue, present := merged[field]
		if !present {
			// Absent means "keep what is stored".
			merged[field] = storedValue
			continue
		}

		// Present and blank also means "keep", same rule secretmerge.KeepMissing
		// follows: the GET is sanitized, so every client reads a blank back and
		// "blank clears" would erase the secret on any round trip. Clearing is
		// done by pointing at a vault credential or retiring the inline field.
		if isBlankSecret(incomingValue) {
			merged[field] = storedValue
		}
	}

	return merged
}

// isBlankSecret reports an empty or whitespace-only string. A non-string is
// never blank: sending a number or an object is deliberate.
func isBlankSecret(value interface{}) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(text) == ""
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

	// Remove sensitive fields
	for _, field := range sensitiveFieldNames {
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
