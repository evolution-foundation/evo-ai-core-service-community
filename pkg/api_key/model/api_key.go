package model

import (
	"strings"
	"time"

	"evo-ai-core-service/pkg/evoextensions/tenantfield"

	"github.com/google/uuid"
)

type ApiKey struct {
	tenantfield.TenantField

	ID       uuid.UUID `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	Name     string    `json:"-" gorm:"not null; type:varchar(255)"`
	Provider string    `json:"-" gorm:"not null; type:varchar(255)"`
	Key      string    `json:"-" gorm:"not null; type:text"`
	KeyHint  string    `json:"-" gorm:"not null; type:varchar(8);default:''"`
	Scope    string    `json:"-" gorm:"not null; type:varchar(32);default:'account'"`
	// The endpoint this credential talks to. NULL/empty means the provider
	// default, which is what every credential registered before migration
	// 000022 meant. An OpenAI-compatible provider is defined by the PAIR
	// key+endpoint, so it lives here and not in installation config.
	BaseURL *string `json:"-" gorm:"type:varchar(512)"`
	// BaseURLSet distinguishes "the request did not mention base_url" from "the
	// request cleared it": GORM skips nil in a struct Updates, so without this
	// flag clearing the endpoint would be a silent no-op. Not a column.
	BaseURLSet bool `json:"-" gorm:"-"`
	// Set only by the 1.5 migration; nil for credentials a human registered.
	ImportedFrom *string   `json:"-" gorm:"type:varchar(64)"`
	IsActive     bool      `json:"-" gorm:"not null; type:boolean;default:true"`
	CreatedAt    time.Time `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt    time.Time `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
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
	Scope    string `json:"scope"`
	Key      string `json:"key"`       // Backend format
	KeyValue string `json:"key_value"` // Frontend format
	BaseURL  string `json:"base_url"`
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
	Scope    string `json:"scope"`
	Key      string `json:"key"`       // Backend format
	KeyValue string `json:"key_value"` // Frontend format
	// Pointer so an omitted base_url keeps the stored endpoint while an
	// explicit "" clears it back to the provider default. A plain string could
	// not tell the two apart, and GORM would skip the empty one anyway.
	BaseURL *string `json:"base_url"`
	// Pointer so "absent" and "false" are distinguishable: GORM's struct
	// Updates skips zero values, so a plain bool could never turn a credential
	// OFF, which made the settings screen's deactivation toggle a silent no-op.
	IsActive *bool `json:"is_active"`
}

// GetKey returns the key, prioritizing key_value (frontend) over key (backend)
func (r *ApiKeyUpdateRequest) GetKey() string {
	if r.KeyValue != "" {
		return r.KeyValue
	}
	return r.Key
}

type ApiKeyResponse struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Provider string    `json:"provider"`
	KeyHint  string    `json:"key_hint"`
	Scope    string    `json:"scope"`
	// Not a secret: the endpoint is what the screen renders back so the user
	// sees where the credential points.
	BaseURL          *string   `json:"base_url,omitempty"`
	ImportedFrom     *string   `json:"imported_from,omitempty"`
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
	Scope    string `json:"-"`
}

const keyHintLength = 4

// Credential scopes. The resolution chain lives in the CRM (Ai::CredentialResolver);
// this service only stores and filters by the scope a credential belongs to.
const (
	ScopeInstallation = "installation"
	ScopeAccount      = "account"
)

// NormalizeScope keeps unknown or missing values on the narrowest scope, so a
// malformed request can never widen a credential to the whole installation.
func NormalizeScope(scope string) string {
	if scope == ScopeInstallation {
		return ScopeInstallation
	}
	return ScopeAccount
}

// openAICompatibleProviders speak the OpenAI wire protocol, so every AI feature
// can use them. The remaining providers are only reachable through AI Agents.
var openAICompatibleProviders = map[string]bool{
	"openai":                   true,
	"azure":                    true,
	"custom":                   true,
	"custom_openai_compatible": true,
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

// NormalizeBaseURL turns the request's endpoint into what the column stores:
// nil for "use the provider default", the trimmed value otherwise. An empty
// string is not stored as empty so every "no endpoint" reads the same way.
func NormalizeBaseURL(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (u *ApiKey) ToResponse() *ApiKeyResponse {
	return &ApiKeyResponse{
		ID:               u.ID,
		Name:             u.Name,
		Provider:         u.Provider,
		KeyHint:          u.KeyHint,
		Scope:            u.Scope,
		BaseURL:          u.BaseURL,
		ImportedFrom:     u.ImportedFrom,
		OpenAICompatible: IsOpenAICompatible(u.Provider),
		IsActive:         u.IsActive,
		CreatedAt:        u.CreatedAt,
		UpdatedAt:        u.UpdatedAt,
	}
}
