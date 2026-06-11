package model

import (
	"evo-ai-core-service/internal/utils/stringutils"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type CustomMcpServer struct {
	ID          uuid.UUID      `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	TenantID    uuid.UUID      `json:"-" gorm:"column:tenant_id;type:uuid;not null;default:'00000000-0000-0000-0000-000000000000'"`
	Name        string         `json:"-" gorm:"not null; type:varchar(255)"`
	Description string         `json:"-" gorm:"type:text"`
	URL         string         `json:"-" gorm:"not null; type:varchar(1024)"`
	Headers     string         `json:"-" gorm:"not null; type:json"`
	Timeout     int            `json:"-" gorm:"not null; type:integer"`
	RetryCount  int            `json:"-" gorm:"not null; type:integer"`
	Tags        pq.StringArray `json:"-" gorm:"not null; type:varchar(255)[]" default:"{}"`
	Tools       string         `json:"-" gorm:"not null; type:json"`
	CreatedAt   time.Time      `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt   time.Time      `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (CustomMcpServer) TableName() string {
	return "evo_core_custom_mcp_servers"
}

type CustomMcpServerBase struct {
	Name        string                   `json:"name" binding:"required"`
	Description string                   `json:"description"`
	URL         string                   `json:"url" binding:"required"`
	Headers     map[string]string        `json:"headers" binding:"required"`
	Timeout     int                      `json:"timeout" binding:"min=0"`
	RetryCount  int                      `json:"retry_count" binding:"min=0"`
	Tags        []string                 `json:"tags" validate:"dive"`
	Tools       []map[string]interface{} `json:"-"`
}

type CustomMcpServerRequest struct {
	CustomMcpServerBase
}

type CustomMcpServerUpdateRequest struct {
	CustomMcpServerBase
}

type CustomMcpServerResponse struct {
	ID          uuid.UUID                `json:"id"`
	Name        string                   `json:"name"`
	Description string                   `json:"description"`
	URL         string                   `json:"url"`
	Headers     map[string]string        `json:"headers"`
	Timeout     int                      `json:"timeout"`
	RetryCount  int                      `json:"retry_count"`
	Tags        []string                 `json:"tags"`
	Tools       []map[string]interface{} `json:"tools"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

type TestResult struct {
	Success      bool    `json:"success"`
	StatusCode   int     `json:"status_code,omitempty"`
	ResponseTime float64 `json:"response_time,omitempty"`
	URLTested    string  `json:"url_tested"`
	Message      string  `json:"message,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type CustomMcpServerTestResponse struct {
	Server     *CustomMcpServerResponse `json:"server"`
	TestResult *TestResult              `json:"test_result"`
}

type CustomMcpServerListRequest struct {
	Page     int    `json:"-" binding:"required"`
	PageSize int    `json:"-" binding:"required"`
	Search   string `json:"-" binding:"required"`
	Tags     string `json:"-"`
}

type CustomMcpServerToolsResponse struct {
	Tools []map[string]interface{} `json:"tools"`
}

type CustomMcpServerListResponse struct {
	Items      []CustomMcpServerResponse `json:"-"`
	Page       int                       `json:"-"`
	PageSize   int                       `json:"-"`
	Skip       int                       `json:"-"`
	Limit      int                       `json:"-"`
	TotalItems int64                     `json:"-"`
	TotalPages int                       `json:"-"`
}

func (u *CustomMcpServer) ToResponse() *CustomMcpServerResponse {
	return &CustomMcpServerResponse{
		ID:          u.ID,
		Name:        u.Name,
		Description: u.Description,
		URL:         u.URL,
		Headers:     stringutils.JSONToStringMap(u.Headers),
		Timeout:     u.Timeout,
		RetryCount:  u.RetryCount,
		Tags:        u.Tags,
		Tools:       stringutils.JSONToInterfaceMapSlice(u.Tools),
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}
