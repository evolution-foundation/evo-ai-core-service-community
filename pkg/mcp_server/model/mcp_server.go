package model

import (
	"evo-ai-core-service/internal/utils/stringutils"
	"time"

	"github.com/google/uuid"
)

type McpServer struct {
	ID           uuid.UUID `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	TenantID     uuid.UUID `json:"-" gorm:"column:tenant_id;type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'"`
	Name         string    `json:"-" gorm:"not null; type:varchar(255)"`
	Description  string    `json:"-" gorm:"type:text"`
	ConfigType   string    `json:"-" gorm:"not null; type:varchar(10)" enums:"studio,sse"`
	ConfigJSON   string    `json:"-" gorm:"not null; type:json"`
	Environments string    `json:"-" gorm:"not null; type:json"`
	Tools        string    `json:"-" gorm:"not null; type:json"`
	Type         string    `json:"-" gorm:"not null; type:varchar(10)" enums:"official,community"`
	CreatedAt    time.Time `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt    time.Time `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (McpServer) TableName() string {
	return "evo_core_mcp_servers"
}

type Tools struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Tags        []string               `json:"tags"`
	Examples    []string               `json:"examples"`
	InputModes  []string               `json:"inputModes"`
	OutputModes []string               `json:"outputModes"`
	Config      map[string]interface{} `json:"config"`
}

type McpServerRequest struct {
	Name         string                 `json:"name" binding:"required"`
	Description  string                 `json:"description"`
	ConfigType   string                 `json:"config_type" binding:"required" example:"studio"`
	ConfigJSON   map[string]interface{} `json:"config_json" binding:"required"`
	Environments map[string]interface{} `json:"environments" binding:"required"`
	Tools        []Tools                `json:"tools" binding:"required"`
	Type         string                 `json:"type" binding:"required" example:"official"`
}

type McpServerResponse struct {
	ID           uuid.UUID              `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description"`
	ConfigType   string                 `json:"config_type" example:"studio"`
	ConfigJSON   map[string]interface{} `json:"config_json"`
	Environments map[string]interface{} `json:"environments"`
	Tools        []Tools                `json:"tools"`
	Type         string                 `json:"type" example:"official"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

type McpServerListResponse struct {
	Items      []McpServerResponse `json:"-"`
	Page       int                 `json:"-"`
	PageSize   int                 `json:"-"`
	Skip       int                 `json:"-"`
	Limit      int                 `json:"-"`
	TotalItems int64               `json:"-"`
	TotalPages int                 `json:"-"`
}

func (u *McpServer) ToResponse() *McpServerResponse {
	return &McpServerResponse{
		ID:           u.ID,
		Name:         u.Name,
		Description:  u.Description,
		ConfigType:   u.ConfigType,
		ConfigJSON:   stringutils.JSONToInterfaceMap(u.ConfigJSON),
		Environments: stringutils.JSONToInterfaceMap(u.Environments),
		Tools:        stringutils.JSONToStruct[Tools](u.Tools),
		Type:         u.Type,
		CreatedAt:    u.CreatedAt,
		UpdatedAt:    u.UpdatedAt,
	}
}
