package model

import (
	"time"

	"github.com/google/uuid"
)

type ApiKey struct {
	ID        uuid.UUID `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	TenantID  uuid.UUID `json:"-" gorm:"column:tenant_id;type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'"`
	Name      string    `json:"-" gorm:"not null; type:varchar(255)"`
	Provider  string    `json:"-" gorm:"not null; type:varchar(255)"`
	Key       string    `json:"-" gorm:"not null; type:text"`
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
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Provider  string    `json:"provider"`
	Key       string    `json:"key"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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

func (u *ApiKey) ToResponse() *ApiKeyResponse {
	return &ApiKeyResponse{
		ID:        u.ID,
		Name:      u.Name,
		Provider:  u.Provider,
		Key:       u.Key,
		IsActive:  u.IsActive,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}
