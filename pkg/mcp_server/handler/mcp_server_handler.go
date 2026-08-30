package handler

import (
	"evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/mcp_server/model"
	"evo-ai-core-service/pkg/mcp_server/service"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// McpServerHandler interface defines the contract for mcp server handlers
type McpServerHandler interface {
	RegisterRoutesMiddleware(router gin.IRouter)
	Create(c *gin.Context)
	GetByID(c *gin.Context)
	List(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
}

// mcpServerHandler implements the McpServerHandler interface.
type mcpServerHandler struct {
	mcpServersService service.McpServerService
}

// NewMcpServerHandler creates a new mcp server handler.
func NewMcpServerHandler(mcpServersService service.McpServerService) McpServerHandler {
	return &mcpServerHandler{
		mcpServersService: mcpServersService,
	}
}

// RegisterRoutesMiddleware registers the routes for the mcp server handler with middleware.
func (h *mcpServerHandler) RegisterRoutesMiddleware(router gin.IRouter) {
	// Usar middleware global de permissão
	permissionMiddleware := middleware.GetGlobalPermissionMiddleware()

	mcpServers := router.Group("/mcp-servers")
	{
		// Read permissions
		mcpServers.GET("",
			permissionMiddleware.RequirePermission("ai_mcp_servers", "read"),
			h.List)
		mcpServers.GET("/",
			permissionMiddleware.RequirePermission("ai_mcp_servers", "read"),
			h.List)
		mcpServers.GET("/:id",
			permissionMiddleware.RequirePermission("ai_mcp_servers", "read"),
			h.GetByID)

		// Write permissions. The auth service owns who may write here:
		// ai_mcp_servers.<action> is granted to super_admin and account_owner by
		// its RBAC seed, so a second local gate can only contradict it.
		mcpServers.POST("",
			permissionMiddleware.RequirePermission("ai_mcp_servers", "create"),
			h.Create)

		mcpServers.PUT("/:id",
			permissionMiddleware.RequirePermission("ai_mcp_servers", "update"),
			h.Update)

		mcpServers.DELETE("/:id",
			permissionMiddleware.RequirePermission("ai_mcp_servers", "delete"),
			h.Delete)
	}
}

// Create handles the create mcp server request.
func (h *mcpServerHandler) Create(c *gin.Context) {
	var req *model.McpServerRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	mcpServers := model.McpServer{
		Name:         req.Name,
		Description:  req.Description,
		ConfigType:   req.ConfigType,
		Type:         req.Type,
		ConfigJSON:   stringutils.InterfaceMapToJSON(req.ConfigJSON),
		Environments: stringutils.InterfaceMapToJSON(req.Environments),
		Tools:        stringutils.ToJSON(req.Tools),
	}

	createdMcpServers, err := h.mcpServersService.Create(c.Request.Context(), mcpServers)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, createdMcpServers.ToResponse(), "MCP server created successfully", http.StatusCreated)
}

// GetByID handles the get mcp server by id request.
func (h *mcpServerHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	mcpServers, err := h.mcpServersService.GetByID(c.Request.Context(), id)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, mcpServers.ToResponse(), "MCP server retrieved successfully", http.StatusOK)
}

// List handles the list mcp servers request.
func (h *mcpServerHandler) List(c *gin.Context) {
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

	listMcpServers, err := h.mcpServersService.List(c.Request.Context(), page, pageSize)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.PaginatedResponse(c, listMcpServers.Items, listMcpServers.Page, listMcpServers.PageSize, int(listMcpServers.TotalItems), "MCP servers retrieved successfully", http.StatusOK)
}

// Update handles the update mcp server request.
func (h *mcpServerHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	var req model.McpServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	mcpServers := &model.McpServer{
		Name:        req.Name,
		Description: req.Description,
		ConfigType:  req.ConfigType,
		Type:        req.Type,
	}

	updatedMcpServers, err := h.mcpServersService.Update(c.Request.Context(), mcpServers, id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, updatedMcpServers.ToResponse(), "MCP server updated successfully", http.StatusOK)
}

// Delete handles the delete mcp server request
func (h *mcpServerHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	_, err = h.mcpServersService.Delete(c.Request.Context(), id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, nil, "MCP server deleted successfully", http.StatusNoContent)
}
