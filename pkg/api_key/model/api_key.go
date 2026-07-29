package model

import (
	"time"

	"evo-ai-core-service/pkg/evoextensions/tenantfield"

	"github.com/google/uuid"
)

type ApiKey struct {
	tenantfield.TenantField

	ID        uuid.UUID `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	Name      string    `json:"-" gorm:"not null; type:varchar(255)"`
	Provider  string    `json:"-" gorm:"not null; type:varchar(255)"`
	Key       string    `json:"-" gorm:"not null; type:text"`
	KeyHint   string    `json:"-" gorm:"not null; type:varchar(8);default:''"`
	IsActive  bool      `json:"-" gorm:"not null; type:boolean;default:true"`
	CreatedAt time.Time `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt time.Time `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (ApiKey) TableName() string {
	return "evo_core_api_keys"
}

type ApiKeyBase struct {
	Name     string `json:"name" binding:"required"`
	Provider string `json:"provider" binding:"required"`
	Key      string `json:"key" binding:"required"`
}

type ApiKeyRequest struct {
	Name     string `json:"name" binding:"required"`
	Provider string `json:"provider" binding:"required"`
	Key      string `json:"key"`       // Backend format
	KeyValue string `json:"key_value"` // Frontend format
}

// GetKey returns the key, prioritizing key_value (frontend) over key (backend)
func (r *ApiKeyRequest) GetKey() string {
	if r.KeyValue != "" {
		return r.KeyValue
	}
	return r.Key
}

type ApiKeyUpdateRequest struct {
	Name     string `json:"name" binding:"required"`
	Provider string `json:"provider" binding:"required"`
	Key      string `json:"key"`       // Backend format
	KeyValue string `json:"key_value"` // Frontend format
}

// GetKey returns the key, prioritizing key_value (frontend) over key (backend)
func (r *ApiKeyUpdateRequest) GetKey() string {
	if r.KeyValue != "" {
		return r.KeyValue
	}
	return r.Key
}

type ApiKeyResponse struct {
	ID               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	Provider         string    `json:"provider"`
	KeyHint          string    `json:"key_hint"`
	OpenAICompatible bool      `json:"openai_compatible"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type ApiKeyListResponse struct {
	Items      []ApiKeyResponse `json:"-"`
	Page       int              `json:"-"`
	PageSize   int              `json:"-"`
	Skip       int              `json:"-"`
	Limit      int              `json:"-"`
	TotalItems int64            `json:"-"`
	TotalPages int              `json:"-"`
}

type ApiKeyListRequest struct {
	Page     int    `json:"-" binding:"required"`
	PageSize int    `json:"-" binding:"required"`
	Active   string `json:"-" binding:"required"`
}

const keyHintLength = 4

// openAICompatibleProviders speak the OpenAI wire protocol, so every AI feature
// can use them. The remaining providers are only reachable through AI Agents.
var openAICompatibleProviders = map[string]bool{
	"openai": true,
	"azure":  true,
	"custom": true,
}

// IsOpenAICompatible reports whether the provider serves every AI feature or
// only AI Agents.
func IsOpenAICompatible(provider string) bool {
	return openAICompatibleProviders[provider]
}

// DeriveKeyHint returns the last characters of a plaintext key, used to render
// a mask in the UI without ever sending the key back to the browser.
func DeriveKeyHint(plainKey string) string {
	runes := []rune(plainKey)
	if len(runes) <= keyHintLength {
		return string(runes)
	}
	return string(runes[len(runes)-keyHintLength:])
}

func (u *ApiKey) ToResponse() *ApiKeyResponse {
	return &ApiKeyResponse{
		ID:               u.ID,
		Name:             u.Name,
		Provider:         u.Provider,
		KeyHint:          u.KeyHint,
		OpenAICompatible: IsOpenAICompatible(u.Provider),
		IsActive:         u.IsActive,
		CreatedAt:        u.CreatedAt,
		UpdatedAt:        u.UpdatedAt,
	}
}
