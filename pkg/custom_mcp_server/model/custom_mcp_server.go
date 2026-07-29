package model

import (
	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/evoextensions/secretmerge"
	"evo-ai-core-service/pkg/evoextensions/tenantfield"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type CustomMcpServer struct {
	tenantfield.TenantField

	ID          uuid.UUID `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	Name        string    `json:"-" gorm:"not null; type:varchar(255)"`
	Description string    `json:"-" gorm:"type:text"`
	URL         string    `json:"-" gorm:"not null; type:varchar(1024)"`
	Headers     string    `json:"-" gorm:"not null; type:json"`
	// Vault references: header name -> credential id (EVO-2250 story 2.4).
	CredentialRefs string         `json:"-" gorm:"not null; type:jsonb;default:'{}'"`
	Timeout        int            `json:"-" gorm:"not null; type:integer"`
	RetryCount     int            `json:"-" gorm:"not null; type:integer"`
	Tags           pq.StringArray `json:"-" gorm:"not null; type:varchar(255)[]" default:"{}"`
	Tools          string         `json:"-" gorm:"not null; type:json"`
	CreatedAt      time.Time      `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt      time.Time      `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (CustomMcpServer) TableName() string {
	return "evo_core_custom_mcp_servers"
}

type CustomMcpServerBase struct {
	Name           string                   `json:"name" binding:"required"`
	Description    string                   `json:"description"`
	URL            string                   `json:"url" binding:"required"`
	Headers        map[string]string        `json:"headers" binding:"required"`
	CredentialRefs map[string]string        `json:"credential_refs"`
	Timeout        int                      `json:"timeout" binding:"min=0"`
	RetryCount     int                      `json:"retry_count" binding:"min=0"`
	Tags           []string                 `json:"tags" validate:"dive"`
	Tools          []map[string]interface{} `json:"-"`
}

type CustomMcpServerRequest struct {
	CustomMcpServerBase
}

type CustomMcpServerUpdateRequest struct {
	CustomMcpServerBase
}

type CustomMcpServerResponse struct {
	ID             uuid.UUID                `json:"id"`
	Name           string                   `json:"name"`
	Description    string                   `json:"description"`
	URL            string                   `json:"url"`
	Headers        map[string]string        `json:"headers"`
	CredentialRefs map[string]string        `json:"credential_refs"`
	Timeout        int                      `json:"timeout"`
	RetryCount     int                      `json:"retry_count"`
	Tags           []string                 `json:"tags"`
	Tools          []map[string]interface{} `json:"tools"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
}

type TestResult struct {
	Success      bool    `json:"success"`
	StatusCode   int     `json:"status_code,omitempty"`
	ResponseTime float64 `json:"response_time,omitempty"`
	URLTested    string  `json:"url_tested"`
	Message      string  `json:"message,omitempty"`
	Error        string  `json:"error,omitempty"`
	// EVO-2139: number of tools discovered in this test's MCP handshake.
	// The UI shows this in the toast instead of `server.tools.length` (which
	// reflects the DB, possibly 0 if the original Create failed to populate).
	// No `omitempty`: a successful test that finds 0 tools is a legitimate 0
	// that must still reach the UI, otherwise the client falls back to the
	// DB-stale length instead of the live handshake count.
	ToolsCount int `json:"tools_count"`
}

type CustomMcpServerTestResponse struct {
	Server     *CustomMcpServerResponse `json:"server"`
	TestResult *TestResult              `json:"test_result"`
}

type CustomMcpServerListRequest struct {
	Page     int                         `json:"-" binding:"required"`
	PageSize int                         `json:"-" binding:"required"`
	Search   string                      `json:"-" binding:"required"`
	Tags     string                      `json:"-"`
	Filters  []CustomMcpServerListFilter `json:"-"`
}

// CustomMcpServerListFilter is one advanced-filter clause from the Custom MCP
// Servers list screen (filters[i][attribute_key|filter_operator|values|query_operator]).
type CustomMcpServerListFilter struct {
	AttributeKey   string
	FilterOperator string
	QueryOperator  string
	Values         []string
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
	// EVO-2139: normaliza Tools para [] em vez de nil para que clients HTTP
	// nunca recebam `"tools": null`. Um server com Tools ainda não descobertas
	// (ex.: falha intermitente no Create) marshalava como null e crashava
	// consumidores que acessavam `.length` diretamente.
	tools := stringutils.JSONToInterfaceMapSlice(u.Tools)
	if tools == nil {
		tools = []map[string]interface{}{}
	}
	return &CustomMcpServerResponse{
		ID:          u.ID,
		Name:        u.Name,
		Description: u.Description,
		URL:         u.URL,
		// Header VALUES are redacted, names survive (EVO-2250 story 2.4).
		Headers:        secretmerge.RedactValues(stringutils.JSONToStringMap(u.Headers)),
		CredentialRefs: stringutils.JSONToStringMap(u.CredentialRefs),
		Timeout:        u.Timeout,
		RetryCount:     u.RetryCount,
		Tags:           u.Tags,
		Tools:          tools,
		CreatedAt:      u.CreatedAt,
		UpdatedAt:      u.UpdatedAt,
	}
}
