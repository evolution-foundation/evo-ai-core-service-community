package model

import (
	folderModel "evo-ai-core-service/pkg/folder/model"
	"time"

	"github.com/google/uuid"
)

type FolderShare struct {
	ID               uuid.UUID          `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	TenantID         uuid.UUID          `json:"-" gorm:"column:tenant_id;type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'"`
	FolderID         uuid.UUID          `json:"-" gorm:"<-:create;type:uuid;not null; type:uuid;references:evo_core_folders(id)"`
	Folder           folderModel.Folder `json:"-" gorm:"foreignKey:FolderID"`
	SharedByUserID   uuid.UUID          `json:"-" gorm:"<-:create;not null;type:uuid"` // Removed foreign key reference to external users table
	SharedWithEmail  string             `json:"-" gorm:"not null; type:varchar(255)"`
	SharedWithUserID uuid.UUID          `json:"-" gorm:"<-:create;not null;type:uuid"` // Removed foreign key reference to external users table
	PermissionLevel  string             `json:"-" gorm:"not null; type:varchar(5)" enums:"read,write"`
	IsActive         bool               `json:"-" gorm:"not null; type:boolean;default:true"`
	CreatedAt        time.Time          `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt        time.Time          `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (FolderShare) TableName() string {
	return "evo_core_folder_shares"
}

type FolderShareRequest struct {
	SharedWithEmail string `json:"shared_with_email" binding:"required"`
	PermissionLevel string `json:"permission_level" binding:"required" enums:"read,write"`
}

type FolderShareResponse struct {
	ID               uuid.UUID `json:"id"`
	FolderID         uuid.UUID `json:"folder_id"`
	SharedByUserID   uuid.UUID `json:"shared_by_user_id"`
	SharedWithEmail  string    `json:"shared_with_email"`
	SharedWithUserID uuid.UUID `json:"shared_with_user_id"`
	PermissionLevel  string    `json:"permission_level"`
	IsActive         bool      `json:"is_active"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type FolderShareUpdateRequest struct {
	PermissionLevel string `json:"permission_level" binding:"required" enums:"read,write"`
	IsActive        *bool  `json:"is_active" binding:"required"`
}

type FolderWithSharingResponse struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	IsShared        bool       `json:"is_shared"`
	PermissionLevel string     `json:"permission_level"`
	SharedBy        *string    `json:"shared_by,omitempty"`
	ShareID         *uuid.UUID `json:"share_id,omitempty"`
}
type FolderWithSharingListResponse struct {
	Items      []FolderWithSharingResponse `json:"-"`
	Page       int                         `json:"-"`
	PageSize   int                         `json:"-"`
	Skip       int                         `json:"-"`
	Limit      int                         `json:"-"`
	TotalItems int64                       `json:"-"`
	TotalPages int                         `json:"-"`
}

type FolderShareListResponse struct {
	Items      []FolderShareResponse `json:"-"`
	Page       int                   `json:"-"`
	PageSize   int                   `json:"-"`
	Skip       int                   `json:"-"`
	Limit      int                   `json:"-"`
	TotalItems int64                 `json:"-"`
	TotalPages int                   `json:"-"`
}

func (f *FolderShare) ToResponse() *FolderShareResponse {
	return &FolderShareResponse{
		ID:               f.ID,
		FolderID:         f.FolderID,
		SharedByUserID:   f.SharedByUserID,
		SharedWithEmail:  f.SharedWithEmail,
		SharedWithUserID: f.SharedWithUserID,
		PermissionLevel:  f.PermissionLevel,
		IsActive:         f.IsActive,
		CreatedAt:        f.CreatedAt,
		UpdatedAt:        f.UpdatedAt,
	}
}
