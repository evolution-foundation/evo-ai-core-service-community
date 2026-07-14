package model

import (
	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/evoextensions/tenantfield"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type CustomTool struct {
	tenantfield.TenantField

	ID            uuid.UUID      `json:"-" gorm:"<-:create;type:uuid;primary_key;default:uuid_generate_v4()"`
	Name          string         `json:"-" gorm:"not null; type:varchar(255)"`
	Description   string         `json:"-" gorm:"type:text"`
	Method        string         `json:"-" gorm:"not null; type:varchar(10)"`
	Endpoint      string         `json:"-" gorm:"not null; type:varchar(1024)"`
	Headers       string         `json:"-" gorm:"not null; type:json"`
	PathParams    string         `json:"-" gorm:"not null; type:json"`
	QueryParams   string         `json:"-" gorm:"not null; type:json"`
	BodyParams    string         `json:"-" gorm:"not null; type:json"`
	ErrorHandling string         `json:"-" gorm:"not null; type:json"`
	Values        string         `json:"-" gorm:"not null; type:json"`
	Tags          pq.StringArray `json:"-" gorm:"not null; type:varchar(255)[]" default:"{}"`
	Examples      pq.StringArray `json:"-" gorm:"not null; type:varchar(255)[]" default:"{}"`
	InputModes    pq.StringArray `json:"-" gorm:"not null; type:varchar(255)[]" default:"{}"`
	OutputModes   pq.StringArray `json:"-" gorm:"not null; type:varchar(255)[]" default:"{}"`
	CreatedAt     time.Time      `json:"-" gorm:"autoCreateTime;not null" default:"now()"`
	UpdatedAt     time.Time      `json:"-" gorm:"autoUpdateTime;not null" default:"now()"`
}

func (CustomTool) TableName() string {
	return "evo_core_custom_tools"
}

type CustomToolBase struct {
	Name          string                 `json:"name" binding:"required"`
	Description   string                 `json:"description"`
	Method        string                 `json:"method" binding:"required" enums:"GET,POST,PUT,DELETE,PATCH,HEAD,OPTIONS"`
	Endpoint      string                 `json:"endpoint" binding:"required"`
	Headers       map[string]string      `json:"headers" binding:"required"`
	PathParams    map[string]string      `json:"path_params" binding:"required"`
	QueryParams   map[string]interface{} `json:"query_params" binding:"required"`
	BodyParams    map[string]interface{} `json:"body_params" binding:"required"`
	ErrorHandling map[string]interface{} `json:"error_handling" binding:"required"`
	Values        map[string]interface{} `json:"values" binding:"required"`
	Tags          []string               `json:"tags" validate:"dive"`
	Examples      []string               `json:"examples" validate:"dive"`
	InputModes    []string               `json:"input_modes" validate:"dive"`
	OutputModes   []string               `json:"output_modes" validate:"dive"`
}

type CustomToolRequest struct {
	CustomToolBase
}

type CustomToolUpdateRequest struct {
	CustomToolBase
}

type CustomToolResponse struct {
	ID            uuid.UUID              `json:"id"`
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Method        string                 `json:"method"`
	Endpoint      string                 `json:"endpoint"`
	Headers       map[string]string      `json:"headers"`
	PathParams    map[string]string      `json:"path_params"`
	QueryParams   map[string]interface{} `json:"query_params"`
	BodyParams    map[string]interface{} `json:"body_params"`
	ErrorHandling map[string]interface{} `json:"error_handling"`
	Values        map[string]interface{} `json:"values"`
	Tags          []string               `json:"tags"`
	Examples      []string               `json:"examples"`
	InputModes    []string               `json:"input_modes"`
	OutputModes   []string               `json:"output_modes"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

type CustomToolListRequest struct {
	Page     int                    `json:"-" binding:"required"`
	PageSize int                    `json:"-" binding:"required"`
	Search   string                 `json:"-" binding:"required"`
	Tags     string                 `json:"-"`
	Filters  []CustomToolListFilter `json:"-"`
}

// CustomToolListFilter is one advanced-filter clause from the Custom Tools list
// screen (filters[i][attribute_key|filter_operator|values|query_operator]).
type CustomToolListFilter struct {
	AttributeKey   string
	FilterOperator string
	QueryOperator  string
	Values         []string
}

type TestResult struct {
	Success      bool              `json:"success"`
	StatusCode   int               `json:"status_code,omitempty"`
	ResponseTime float64           `json:"response_time,omitempty"`
	Headers      map[string]string `json:"headers,omitempty"`
	Body         string            `json:"body,omitempty"`
	Error        string            `json:"error,omitempty"`
}

type CustomToolTestResponse struct {
	Tool       *CustomToolResponse `json:"tool"`
	TestResult *TestResult         `json:"test_result"`
}

type CustomToolListResponse struct {
	Items      []CustomToolResponse `json:"-"`
	Page       int                  `json:"-"`
	PageSize   int                  `json:"-"`
	Skip       int                  `json:"-"`
	Limit      int                  `json:"-"`
	TotalItems int64                `json:"-"`
	TotalPages int                  `json:"-"`
}

func (u *CustomTool) ToResponse() *CustomToolResponse {
	return &CustomToolResponse{
		ID:            u.ID,
		Name:          u.Name,
		Description:   u.Description,
		Method:        u.Method,
		Endpoint:      u.Endpoint,
		Headers:       stringutils.JSONToStringMap(u.Headers),
		PathParams:    stringutils.JSONToStringMap(u.PathParams),
		QueryParams:   stringutils.JSONToInterfaceMap(u.QueryParams),
		BodyParams:    stringutils.JSONToInterfaceMap(u.BodyParams),
		ErrorHandling: stringutils.JSONToInterfaceMap(u.ErrorHandling),
		Values:        stringutils.JSONToInterfaceMap(u.Values),
		Tags:          u.Tags,
		Examples:      u.Examples,
		InputModes:    u.InputModes,
		OutputModes:   u.OutputModes,
		CreatedAt:     u.CreatedAt,
		UpdatedAt:     u.UpdatedAt,
	}
}
