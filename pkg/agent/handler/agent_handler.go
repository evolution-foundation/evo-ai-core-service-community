package handler

import (
	"encoding/json"
	"evo-ai-core-service/internal/config"
	"evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/internal/utils/stringutils"
	"evo-ai-core-service/pkg/agent/model"
	"evo-ai-core-service/pkg/agent/service"
	folderShareService "evo-ai-core-service/pkg/folder_share/service"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AgentHandler interface defines the contract for agent handlers
type AgentHandler interface {
	RegisterRoutesMiddleware(router gin.IRouter)
	Create(c *gin.Context)
	GetByID(c *gin.Context)
	List(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
	ImportAgents(c *gin.Context)
	ListAgentsByFolderID(c *gin.Context)
	GetSharedAgent(c *gin.Context)
	GetShareAgent(c *gin.Context)
	AssignFolder(c *gin.Context)
}

// agentHandler implements the AgentHandler interface
type agentHandler struct {
	agentService       service.AgentService
	aiProcessorURL     string
	folderShareService folderShareService.FolderShareService
}

// NewAgentHandler creates a new agent handler
func NewAgentHandler(agentService service.AgentService, aiProcessorServiceConfig *config.AIProcessorServiceConfig, folderShareService folderShareService.FolderShareService) AgentHandler {
	return &agentHandler{
		agentService:       agentService,
		aiProcessorURL:     aiProcessorServiceConfig.URL,
		folderShareService: folderShareService,
	}
}

// RegisterRoutesMiddleware registers the routes for the agent handler with middleware
func (h *agentHandler) RegisterRoutesMiddleware(router gin.IRouter) {
	// Usar middleware global de permissão
	permissionMiddleware := middleware.GetGlobalPermissionMiddleware()

	agents := router.Group("/agents")
	{
		agentAccessMiddleware := middleware.NewAgentAccessMiddleware(h.agentService).GetAgentAccessMiddleware()

		// Read permissions
		agents.GET("",
			permissionMiddleware.RequirePermission("ai_agents", "read"),
			h.List)
		agents.GET("/",
			permissionMiddleware.RequirePermission("ai_agents", "read"),
			h.List)
		agents.GET("/:id",
			permissionMiddleware.RequirePermission("ai_agents", "read"),
			agentAccessMiddleware,
			h.GetByID)
		agents.GET("/:id/shared",
			permissionMiddleware.RequirePermission("ai_agents", "read"),
			h.GetSharedAgent)
		agents.GET("/:id/share",
			permissionMiddleware.RequirePermission("ai_agents", "share"),
			agentAccessMiddleware,
			h.GetShareAgent)

		// Create permissions
		agents.POST("",
			permissionMiddleware.RequirePermission("ai_agents", "create"),
			h.Create)
		agents.POST("/",
			permissionMiddleware.RequirePermission("ai_agents", "create"),
			h.Create)

		// Import permissions
		agents.POST("/import",
			permissionMiddleware.RequirePermission("ai_agents", "import"),
			h.ImportAgents)

		// Update permissions
		agents.PUT("/:id",
			permissionMiddleware.RequirePermission("ai_agents", "update"),
			agentAccessMiddleware,
			h.Update)
		agents.PUT("/:id/folder",
			permissionMiddleware.RequirePermission("ai_agents", "manage_folder"),
			h.AssignFolder)

		// Delete permissions
		agents.DELETE("/:id",
			permissionMiddleware.RequirePermission("ai_agents", "delete"),
			agentAccessMiddleware,
			h.Delete)

	}

	folders := router.Group("/agents/folders")
	{
		folders.GET("/:id/agents",
			permissionMiddleware.RequirePermission("ai_agents", "read"),
			h.ListAgentsByFolderID)
	}
}

// Create handles the create agent request
func (h *agentHandler) Create(c *gin.Context) {
	var req *model.AgentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	agent := model.Agent{
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		Model:       req.Model,
		ApiKeyID:    req.ApiKeyID.UUID,
		Instruction: req.Instruction,
		CardURL:     req.CardURL,
		FolderID:    req.FolderID.UUID,
		Config:      stringutils.InterfaceMapToJSON(req.Config),
		Role:        req.Role,
		Goal:        req.Goal,
	}

	createdAgent, err := h.agentService.Create(c.Request.Context(), agent)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, createdAgent.ToResponse(h.aiProcessorURL), "Agent created successfully", http.StatusCreated)
}

// GetByID handles the get agent by id request
func (h *agentHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	agent, err := h.agentService.GetByID(c.Request.Context(), id)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, agent.ToResponse(h.aiProcessorURL), "Agent retrieved successfully", http.StatusOK)
}

// List handles the list agents request
var agentFilterKeyPattern = regexp.MustCompile(`^filters\[(\d+)\]\[(\w+)\]$`)

// parseAgentFilters reads the advanced-filter payload the Agents list screen
// sends in bracket query params (filters[0][attribute_key]=name&...), ordered
// by index. Unknown attribute keys are dropped later by the repository whitelist.
func parseAgentFilters(c *gin.Context) []model.AgentListFilter {
	byIndex := map[int]*model.AgentListFilter{}

	for key, values := range c.Request.URL.Query() {
		matches := agentFilterKeyPattern.FindStringSubmatch(key)
		if matches == nil || len(values) == 0 {
			continue
		}

		index, err := strconv.Atoi(matches[1])
		if err != nil {
			continue
		}

		filter, ok := byIndex[index]
		if !ok {
			filter = &model.AgentListFilter{}
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

	filters := make([]model.AgentListFilter, 0, len(indices))
	for _, index := range indices {
		filters = append(filters, *byIndex[index])
	}
	return filters
}

func (h *agentHandler) List(c *gin.Context) {
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

	filters := parseAgentFilters(c)
	search := c.Query("search")

	listAgents, err := h.agentService.List(c.Request.Context(), page, pageSize, filters, search)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.PaginatedResponse(c, listAgents.Items, listAgents.Page, listAgents.PageSize, int(listAgents.TotalItems), "Agents retrieved successfully", http.StatusOK)
}

// Update handles the update agent request
func (h *agentHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	var req model.AgentUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	agent := &model.Agent{
		Name:        req.Name,
		Description: req.Description,
		Type:        req.Type,
		Model:       req.Model,
		ApiKeyID:    req.ApiKeyID.UUID,
		Instruction: req.Instruction,
		CardURL:     req.CardURL,
		FolderID:    req.FolderID.UUID,
		Config:      stringutils.InterfaceMapToJSON(req.Config),
		Role:        req.Role,
		Goal:        req.Goal,
	}

	updatedAgent, err := h.agentService.Update(c.Request.Context(), agent, id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, updatedAgent.ToResponse(h.aiProcessorURL), "Agent updated successfully", http.StatusOK)
}

// Delete handles the delete agent request
func (h *agentHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	_, err = h.agentService.Delete(c.Request.Context(), id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, nil, "Agent deleted successfully", http.StatusNoContent)
}

// GetSharedAgent handles the get shared agent request
func (h *agentHandler) GetSharedAgent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	apiKey := c.GetHeader("x-api-key")
	sharedAgent, err := h.agentService.GetSharedAgent(c.Request.Context(), id, apiKey)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, sharedAgent.ToResponse(h.aiProcessorURL), "Shared agent retrieved successfully", http.StatusOK)
}

// GetShareAgent handles the get share agent request
func (h *agentHandler) GetShareAgent(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	apiKey, err := h.agentService.GetShareAgent(c.Request.Context(), id)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, map[string]string{"api_key": apiKey}, "Agent share API key retrieved successfully", http.StatusOK)
}

// ImportAgents handles the import of one or more agents from a JSON file
func (h *agentHandler) ImportAgents(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		response.ErrorResponse(c, errors.BadRequest, err.Error(), nil, http.StatusBadRequest)
		return
	}

	if !strings.EqualFold(filepath.Ext(file.Filename), ".json") {
		response.ErrorResponse(c, errors.BadRequest, "Invalid file type. Only JSON files are allowed.", nil, http.StatusBadRequest)
		return
	}

	// Handle folder_id
	var folderID *uuid.UUID

	folderIDStr := c.PostForm("folder_id")

	if folderIDStr != "" {
		id, err := uuid.Parse(folderIDStr)
		if err != nil {
			response.ErrorResponse(c, errors.BadRequest, err.Error(), nil, http.StatusBadRequest)
			return
		}
		folderID = &id
	}

	fileContent, err := readMultipartFile(file)
	if err != nil {
		response.ErrorResponse(c, errors.BadRequest, err.Error(), nil, http.StatusBadRequest)
		return
	}

	var agentsData []map[string]interface{}
	if err := json.Unmarshal(fileContent, &agentsData); err != nil {
		response.ErrorResponse(c, errors.BadRequest, err.Error(), nil, http.StatusBadRequest)
		return
	}

	AgentImportRequest := model.AgentImportRequest{
		FolderID:  folderID,
		AgentData: agentsData,
	}

	importedAgents, err := h.agentService.ImportAgents(c.Request.Context(), AgentImportRequest)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, importedAgents, "Agents imported successfully", http.StatusCreated)
}

// readMultipartFile reads the content of a multipart file
func readMultipartFile(file *multipart.FileHeader) ([]byte, error) {
	src, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	return io.ReadAll(src)
}

// AssignFolder handles the assign folder request
func (h *agentHandler) AssignFolder(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	var req model.AgentFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	agent := &model.Agent{
		FolderID: req.FolderID,
	}

	agentUpdated, err := h.agentService.AssignFolder(c.Request.Context(), id, agent)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, agentUpdated.ToResponse(h.aiProcessorURL), "Agent folder assigned successfully", http.StatusOK)
}

// ListAgentsByFolderID handles the list agents by folder id request
func (h *agentHandler) ListAgentsByFolderID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

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

	agents, err := h.agentService.ListAgentsByFolderID(c.Request.Context(), id, page, pageSize)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, agents, "Agents retrieved successfully", http.StatusOK)
}
