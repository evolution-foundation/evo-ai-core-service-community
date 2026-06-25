package model

import (
	"time"

	"evo-ai-core-service/pkg/evoextensions/tenantfield"

	"github.com/google/uuid"
)

type Folder struct {
	tenantfield.TenantField

	ID          uuid.UUID `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	Name        string    `json:"-" gorm:"not null; type:varchar(255)"`
	Description string    `json:"-" gorm:"not null; type:text"`
	CreatedAt   time.Time `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt   time.Time `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (Folder) TableName() string {
	return "evo_core_folders"
}

type FolderBase struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

type FolderRequest struct {
	FolderBase
}

type FolderUpdateRequest struct {
	FolderBase
}

type FolderResponse struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type FolderListResponse struct {
	Items      []FolderResponse `json:"-"`
	Page       int              `json:"-"`
	PageSize   int              `json:"-"`
	Skip       int              `json:"-"`
	Limit      int              `json:"-"`
	TotalItems int64            `json:"-"`
	TotalPages int              `json:"-"`
}

func (u *Folder) ToResponse() *FolderResponse {
	return &FolderResponse{
		ID:          u.ID,
		Name:        u.Name,
		Description: u.Description,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}
