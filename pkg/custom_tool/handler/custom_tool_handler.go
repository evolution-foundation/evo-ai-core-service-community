package handler

import (
	"evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/custom_tool/model"
	"evo-ai-core-service/pkg/custom_tool/service"
	"net/http"
	"regexp"
	"sort"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CustomToolHandler interface defines the contract for custom tool handlers
type CustomToolHandler interface {
	RegisterRoutesMiddleware(router gin.IRouter)
	Create(c *gin.Context)
	GetByID(c *gin.Context)
	List(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
	Test(c *gin.Context)
}

// customToolHandler implements the CustomToolHandler interface.
type customToolHandler struct {
	customToolService service.CustomToolService
}

// NewCustomToolHandler creates a new custom tool handler.
func NewCustomToolHandler(customToolService service.CustomToolService) CustomToolHandler {
	return &customToolHandler{
		customToolService: customToolService,
	}
}

// RegisterRoutesMiddleware registers the routes for the custom tool handler with middleware
func (h *customToolHandler) RegisterRoutesMiddleware(router gin.IRouter) {
	// Usar middleware global de permissão
	permissionMiddleware := middleware.GetGlobalPermissionMiddleware()

	customTools := router.Group("/custom-tools")
	{
		// Read permissions
		customTools.GET("",
			permissionMiddleware.RequirePermission("ai_custom_tools", "read"),
			h.List)
		customTools.GET("/",
			permissionMiddleware.RequirePermission("ai_custom_tools", "read"),
			h.List)
		customTools.GET("/:id",
			permissionMiddleware.RequirePermission("ai_custom_tools", "read"),
			h.GetByID)

		// Create permissions
		customTools.POST("",
			permissionMiddleware.RequirePermission("ai_custom_tools", "create"),
			h.Create)
		customTools.POST("/",
			permissionMiddleware.RequirePermission("ai_custom_tools", "create"),
			h.Create)

		// Update permissions
		customTools.PUT("/:id",
			permissionMiddleware.RequirePermission("ai_custom_tools", "update"),
			h.Update)

		// Delete permissions
		customTools.DELETE("/:id",
			permissionMiddleware.RequirePermission("ai_custom_tools", "delete"),
			h.Delete)

		// Test permissions
		customTools.GET("/:id/test",
			permissionMiddleware.RequirePermission("ai_custom_tools", "read"),
			h.Test)
	}
}

// Create handles the create custom tool request
func (h *customToolHandler) Create(c *gin.Context) {
	var req *model.CustomToolRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	customTools := model.CustomTool{
		Name:          req.Name,
		Description:   req.Description,
		Method:        req.Method,
		Endpoint:      req.Endpoint,
		Headers:       stringutils.StringMapToJSON(req.Headers),
		PathParams:    stringutils.StringMapToJSON(req.PathParams),
		QueryParams:   stringutils.InterfaceMapToJSON(req.QueryParams),
		BodyParams:    stringutils.InterfaceMapToJSON(req.BodyParams),
		ErrorHandling: stringutils.InterfaceMapToJSON(req.ErrorHandling),
		Values:        stringutils.InterfaceMapToJSON(req.Values),
		Tags:          req.Tags,
		Examples:      req.Examples,
		InputModes:    req.InputModes,
		OutputModes:   req.OutputModes,
	}

	createdCustomTools, err := h.customToolService.Create(c.Request.Context(), customTools)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, createdCustomTools.ToResponse(), "Custom tool created successfully", http.StatusCreated)
}

// GetByID handles the get custom tool by id request
func (h *customToolHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	customTools, err := h.customToolService.GetByID(c.Request.Context(), id)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, customTools.ToResponse(), "Custom tool retrieved successfully", http.StatusOK)
}

// List handles the list custom tools request
var customToolFilterKeyPattern = regexp.MustCompile(`^filters\[(\d+)\]\[(\w+)\]$`)

// parseCustomToolFilters reads the advanced-filter payload the Custom Tools list
// screen sends in bracket query params (filters[0][attribute_key]=name&...),
// ordered by index. Unknown attribute keys are dropped later by the whitelist.
func parseCustomToolFilters(c *gin.Context) []model.CustomToolListFilter {
	byIndex := map[int]*model.CustomToolListFilter{}

	for key, values := range c.Request.URL.Query() {
		matches := customToolFilterKeyPattern.FindStringSubmatch(key)
		if matches == nil || len(values) == 0 {
			continue
		}

		index, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		filter, ok := byIndex[index]
		if !ok {
			filter = &model.CustomToolListFilter{}
			byIndex[index] = filter
		}

		switch matches[2] {
		case "attribute_key":
			filter.AttributeKey = values[0]
		case "filter_operator":
			filter.FilterOperator = values[0]
		case "query_operator":
			filter.QueryOperator = values[0]
		case "values":
			filter.Values = []string{values[0]}
		}
	}

	indices := make([]int, 0, len(byIndex))
	for index := range byIndex {
		indices = append(indices, index)
	}
	sort.Ints(indices)

	filters := make([]model.CustomToolListFilter, 0, len(indices))
	for _, index := range indices {
		filters = append(filters, *byIndex[index])
	}
	return filters
}

func (h *customToolHandler) List(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	pageSize, err := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	if skip := c.Query("skip"); skip != "" {
		skipVal, err := strconv.Atoi(skip)
		if err != nil {
			code, message, httpCode := errors.HandleError(err)
			response.ErrorResponse(c, code, message, nil, httpCode)
			return
		}

		limitVal, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
		if err != nil {
			code, message, httpCode := errors.HandleError(err)
			response.ErrorResponse(c, code, message, nil, httpCode)
			return
		}

		page = (skipVal / limitVal) + 1
		pageSize = limitVal
	}

	search := c.DefaultQuery("search", "")
	tags := c.DefaultQuery("tags", "")

	var req model.CustomToolListRequest
	req.Page = page
	req.PageSize = pageSize
	req.Search = search
	req.Tags = tags
	req.Filters = parseCustomToolFilters(c)

	listCustomTools, err := h.customToolService.List(c.Request.Context(), req)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.PaginatedResponse(c, listCustomTools.Items, listCustomTools.Page, listCustomTools.PageSize, int(listCustomTools.TotalItems), "Custom tools retrieved successfully", http.StatusOK)
}

// Update handles the update custom tool request
func (h *customToolHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	var req model.CustomToolUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	customTools := &model.CustomTool{
		Name:          req.Name,
		Description:   req.Description,
		Method:        req.Method,
		Endpoint:      req.Endpoint,
		Headers:       stringutils.StringMapToJSON(req.Headers),
		PathParams:    stringutils.StringMapToJSON(req.PathParams),
		QueryParams:   stringutils.InterfaceMapToJSON(req.QueryParams),
		BodyParams:    stringutils.InterfaceMapToJSON(req.BodyParams),
		ErrorHandling: stringutils.InterfaceMapToJSON(req.ErrorHandling),
		Values:        stringutils.InterfaceMapToJSON(req.Values),
		Tags:          req.Tags,
		Examples:      req.Examples,
		InputModes:    req.InputModes,
		OutputModes:   req.OutputModes,
	}

	updatedCustomTools, err := h.customToolService.Update(c.Request.Context(), customTools, id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, updatedCustomTools.ToResponse(), "Custom tool updated successfully", http.StatusOK)
}

// Delete handles the delete custom tool request
func (h *customToolHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	_, err = h.customToolService.Delete(c.Request.Context(), id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, nil, "Custom tool deleted successfully", http.StatusNoContent)
}

// Test handles the test custom tool request
func (h *customToolHandler) Test(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	customTool, err := h.customToolService.Test(c.Request.Context(), id)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, customTool, "Custom tool test completed successfully", http.StatusOK)
}
