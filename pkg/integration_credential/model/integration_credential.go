package model

import (
	"encoding/json"
	"fmt"
	"time"

	"evo-ai-core-service/pkg/evoextensions/tenantfield"

	"github.com/google/uuid"
)

// IntegrationCredential is the vault entry for a tool or integration secret:
// the Dify key, the n8n basic auth, an MCP header, the Knowledge Nexus key.
// It is deliberately NOT evo_core_api_keys, which holds model-provider keys as
// a simple (provider, key) pair. Integration secrets are free-form and come in
// kinds with different lifecycles, so they get their own registry.
//
// Every field carries `json:"-"`: the entity is written by GORM and read by the
// service, never serialized to a client. Responses go through ToResponse, which
// is what keeps the ciphertext from reaching a browser.
type IntegrationCredential struct {
	tenantfield.TenantField

	ID       uuid.UUID `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	Name     string    `json:"-" gorm:"not null; type:varchar(255)"`
	Provider string    `json:"-" gorm:"not null; type:varchar(100)"`
	Kind     string    `json:"-" gorm:"not null; type:varchar(16);default:'static'"`
	// Fernet ciphertext, and only for KindStatic. A KindOAuth row keeps this
	// empty by database CHECK: the vault never owns a token whose refresh
	// rotates it elsewhere, it points at the store that does.
	Value       string `json:"-" gorm:"type:text"`
	ValueFormat string `json:"-" gorm:"not null; type:varchar(16);default:'scalar'"`
	ValueHint   string `json:"-" gorm:"not null; type:varchar(8);default:''"`
	Scope       string `json:"-" gorm:"not null; type:varchar(32);default:'account'"`
	// Set together, and only for KindOAuth: which store owns the token and
	// which row in it. Story 2.5 fills these.
	OwnerStore *string `json:"-" gorm:"type:varchar(64)"`
	OwnerRef   *string `json:"-" gorm:"type:varchar(128)"`
	// Set only by the 2.6 migration; nil for credentials a human registered.
	ImportedFrom *string   `json:"-" gorm:"type:varchar(128)"`
	IsActive     bool      `json:"-" gorm:"not null; type:boolean;default:true"`
	CreatedAt    time.Time `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt    time.Time `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (IntegrationCredential) TableName() string {
	return "evo_core_integration_credentials"
}

// Credential kinds. The rule the whole epic rests on: the vault stores the
// VALUE of a credential with no lifecycle, and a credential WITH a lifecycle
// enters by REFERENCE, leaving refresh ownership where it already is.
const (
	KindStatic = "static"
	KindOAuth  = "oauth"
)

// Value formats. A composite secret is an indivisible pair (a basic auth login),
// serialized as a small JSON object before encryption. Independent secrets are
// separate credentials, never packed into one envelope.
const (
	ValueFormatScalar    = "scalar"
	ValueFormatComposite = "composite"
)

// Credential scopes. The resolution chain lives in the CRM
// (Ai::IntegrationCredentialResolver); this service only stores and filters by
// the scope a credential belongs to.
const (
	ScopeInstallation = "installation"
	ScopeAccount      = "account"
)

const valueHintLength = 4

// compositeSecretField is the component of a composite envelope the hint is
// derived from. Naming it explicitly is what keeps the hint off the public half
// of the pair: a positional guess would mask the username on some payloads and
// the password on others.
const compositeSecretField = "password"

// NormalizeScope keeps unknown or missing values on the narrowest scope, so a
// malformed request can never widen a credential to the whole installation.
func NormalizeScope(scope string) string {
	if scope == ScopeInstallation {
		return ScopeInstallation
	}
	return ScopeAccount
}

// NormalizeKind keeps unknown or missing values on the kind that carries a
// value, so a typo can never produce a row the CHECK constraint rejects with a
// raw database error.
func NormalizeKind(kind string) string {
	if kind == KindOAuth {
		return KindOAuth
	}
	return KindStatic
}

// NormalizeValueFormat defaults to the single-value shape, so an unrecognized
// format never turns a plain secret into an envelope the readers cannot parse.
func NormalizeValueFormat(format string) string {
	if format == ValueFormatComposite {
		return ValueFormatComposite
	}
	return ValueFormatScalar
}

// DeriveValueHint returns the last characters of a plaintext secret, used to
// render a mask in the UI without ever sending the secret back to the browser.
func DeriveValueHint(plainValue string) string {
	runes := []rune(plainValue)
	if len(runes) <= valueHintLength {
		return string(runes)
	}
	return string(runes[len(runes)-valueHintLength:])
}

// DeriveCompositeHint reads the sensitive component out of a composite envelope
// and hints on that. Hinting on the serialized envelope would show JSON syntax,
// and hinting on the public component would mask the wrong half of the pair.
func DeriveCompositeHint(envelope string) (string, error) {
	var decoded map[string]string
	if err := json.Unmarshal([]byte(envelope), &decoded); err != nil {
		return "", fmt.Errorf("composite value must be a JSON object of string fields: %w", err)
	}

	secret := decoded[compositeSecretField]
	if secret == "" {
		return "", fmt.Errorf("composite value requires a non-empty %q field", compositeSecretField)
	}

	return DeriveValueHint(secret), nil
}

type IntegrationCredentialRequest struct {
	Name        string `json:"name" binding:"required"`
	Provider    string `json:"provider" binding:"required"`
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	ValueFormat string `json:"value_format"`
	Value       string `json:"value"`
}

type IntegrationCredentialUpdateRequest struct {
	Name        string `json:"name" binding:"required"`
	Provider    string `json:"provider" binding:"required"`
	Kind        string `json:"kind"`
	Scope       string `json:"scope"`
	ValueFormat string `json:"value_format"`
	Value       string `json:"value"`
}

// IntegrationCredentialResponse carries the hint and never the value, in either
// form. Story 1.1 established the same shape for AI credentials.
type IntegrationCredentialResponse struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Provider     string    `json:"provider"`
	Kind         string    `json:"kind"`
	ValueFormat  string    `json:"value_format"`
	ValueHint    string    `json:"value_hint"`
	Scope        string    `json:"scope"`
	OwnerStore   *string   `json:"owner_store,omitempty"`
	OwnerRef     *string   `json:"owner_ref,omitempty"`
	ImportedFrom *string   `json:"imported_from,omitempty"`
	IsActive     bool      `json:"is_active"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type IntegrationCredentialListResponse struct {
	Items      []IntegrationCredentialResponse `json:"-"`
	Page       int                             `json:"-"`
	PageSize   int                             `json:"-"`
	Skip       int                             `json:"-"`
	Limit      int                             `json:"-"`
	TotalItems int64                           `json:"-"`
	TotalPages int                             `json:"-"`
}

type IntegrationCredentialListRequest struct {
	Page     int    `json:"-" binding:"required"`
	PageSize int    `json:"-" binding:"required"`
	Active   string `json:"-"`
	Scope    string `json:"-"`
	Kind     string `json:"-"`
	Provider string `json:"-"`
}

func (c *IntegrationCredential) ToResponse() *IntegrationCredentialResponse {
	return &IntegrationCredentialResponse{
		ID:           c.ID,
		Name:         c.Name,
		Provider:     c.Provider,
		Kind:         c.Kind,
		ValueFormat:  c.ValueFormat,
		ValueHint:    c.ValueHint,
		Scope:        c.Scope,
		OwnerStore:   c.OwnerStore,
		OwnerRef:     c.OwnerRef,
		ImportedFrom: c.ImportedFrom,
		IsActive:     c.IsActive,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
	}
}
