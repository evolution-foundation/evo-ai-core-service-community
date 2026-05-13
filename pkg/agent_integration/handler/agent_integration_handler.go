package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/pkg/agent_integration/model"
	"evo-ai-core-service/pkg/agent_integration/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AgentIntegrationHandler interface defines the contract for agent integration handlers
type AgentIntegrationHandler interface {
	RegisterRoutesMiddleware(router gin.IRouter)
	Upsert(c *gin.Context)
	GetByProvider(c *gin.Context)
	ListByAgent(c *gin.Context)
	Delete(c *gin.Context)
	ListKnowledgeNexusSpaces(c *gin.Context)
}

// agentIntegrationHandler implements the AgentIntegrationHandler interface
type agentIntegrationHandler struct {
	service service.AgentIntegrationService
}

// NewAgentIntegrationHandler creates a new agent integration handler
func NewAgentIntegrationHandler(service service.AgentIntegrationService) AgentIntegrationHandler {
	return &agentIntegrationHandler{
		service: service,
	}
}

// RegisterRoutesMiddleware registers the routes for the agent integration handler with middleware
func (h *agentIntegrationHandler) RegisterRoutesMiddleware(router gin.IRouter) {
	// Get global permission middleware
	permissionMiddleware := middleware.GetGlobalPermissionMiddleware()

	// Routes for agent integrations - using :id instead of :agentId to avoid conflict
	integrations := router.Group("/agents/:id/integrations")
	{
		// List all integrations for an agent
		integrations.GET("",
			permissionMiddleware.RequirePermission("ai_agents", "read"),
			h.ListByAgent)
		integrations.GET("/",
			permissionMiddleware.RequirePermission("ai_agents", "read"),
			h.ListByAgent)

		// Get integration by provider
		integrations.GET("/:provider",
			permissionMiddleware.RequirePermission("ai_agents", "read"),
			h.GetByProvider)

		// Upsert integration
		integrations.POST("",
			permissionMiddleware.RequirePermission("ai_agents", "update"),
			h.Upsert)
		integrations.POST("/",
			permissionMiddleware.RequirePermission("ai_agents", "update"),
			h.Upsert)

		// Delete integration
		integrations.DELETE("/:provider",
			permissionMiddleware.RequirePermission("ai_agents", "update"),
			h.Delete)
	}

	// Provider-specific helper endpoints (no agent_id scope — used by the
	// Agent Builder dialog to discover remote resources before saving an
	// integration). Kept under /integrations/* to keep all integration-related
	// surface together without shadowing the /:id/:provider params above.
	integrationHelpers := router.Group("/integrations/knowledge-nexus")
	{
		integrationHelpers.POST("/list-spaces",
			permissionMiddleware.RequirePermission("ai_agents", "update"),
			h.ListKnowledgeNexusSpaces)
	}
}

// Upsert handles the upsert agent integration request
func (h *agentIntegrationHandler) Upsert(c *gin.Context) {
	agentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	var req model.AgentIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	integration, err := h.service.Upsert(c.Request.Context(), agentID, req)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, integration, "Agent integration upserted successfully", http.StatusOK)
}

// GetByProvider handles the get agent integration by provider request
func (h *agentIntegrationHandler) GetByProvider(c *gin.Context) {
	agentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	provider := c.Param("provider")
	if provider == "" {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	integration, err := h.service.GetByProvider(c.Request.Context(), agentID, provider)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, integration, "Agent integration retrieved successfully", http.StatusOK)
}

// ListByAgent handles the list agent integrations request
func (h *agentIntegrationHandler) ListByAgent(c *gin.Context) {
	agentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	integrations, err := h.service.ListByAgent(c.Request.Context(), agentID)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, integrations, "Agent integrations retrieved successfully", http.StatusOK)
}

// Delete handles the delete agent integration request
func (h *agentIntegrationHandler) Delete(c *gin.Context) {
	agentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	provider := c.Param("provider")
	if provider == "" {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	err = h.service.Delete(c.Request.Context(), agentID, provider)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, nil, "Agent integration deleted successfully", http.StatusNoContent)
}

// listKnowledgeNexusSpacesRequest is the body schema for the
// POST /integrations/knowledge-nexus/list-spaces proxy endpoint.
type listKnowledgeNexusSpacesRequest struct {
	NexusBaseURL string `json:"nexus_base_url" binding:"required"`
	NexusAPIKey  string `json:"nexus_api_key"  binding:"required"`
}

// ListKnowledgeNexusSpaces proxies a GET to the user's EvoNexus instance to
// list available knowledge spaces. This exists because the Nexus dashboard
// does not emit CORS headers, so the browser cannot call it directly from the
// Agent Builder dialog. The endpoint never persists the credentials; they are
// only used to build the upstream request and are discarded after the call.
func (h *agentIntegrationHandler) ListKnowledgeNexusSpaces(c *gin.Context) {
	var req listKnowledgeNexusSpacesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationErrorResponse(c, err)
		return
	}

	baseURL := strings.TrimRight(strings.TrimSpace(req.NexusBaseURL), "/")
	apiKey := strings.TrimSpace(req.NexusAPIKey)
	if baseURL == "" || apiKey == "" {
		response.ErrorResponse(
			c,
			"validation_error",
			"nexus_base_url and nexus_api_key are required",
			nil,
			http.StatusBadRequest,
		)
		return
	}

	upstreamURL := baseURL + "/api/knowledge/v1/spaces"
	httpReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		response.ErrorResponse(c, "bad_request", err.Error(), nil, http.StatusBadRequest)
		return
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		response.ErrorResponse(
			c,
			"upstream_unreachable",
			"could not reach Nexus: "+err.Error(),
			nil,
			http.StatusBadGateway,
		)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusOK:
		// Forward the upstream payload (typically {"spaces": [...]}) verbatim.
		var payload map[string]interface{}
		if err := json.Unmarshal(body, &payload); err != nil {
			response.ErrorResponse(
				c,
				"invalid_upstream_response",
				"Nexus returned a non-JSON response",
				nil,
				http.StatusBadGateway,
			)
			return
		}
		response.SuccessResponse(c, payload, "Nexus spaces retrieved successfully", http.StatusOK)
	case http.StatusUnauthorized:
		response.ErrorResponse(
			c,
			"unauthorized",
			"Nexus rejected the API key",
			nil,
			http.StatusUnauthorized,
		)
	case http.StatusForbidden:
		response.ErrorResponse(
			c,
			"forbidden",
			"Nexus API key does not have permission to list spaces",
			nil,
			http.StatusForbidden,
		)
	case http.StatusTooManyRequests:
		response.ErrorResponse(
			c,
			"rate_limited",
			"Nexus rate limit exceeded",
			nil,
			http.StatusTooManyRequests,
		)
	default:
		// Surface the upstream status + a snippet so the user can debug.
		snippet := string(body)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		response.ErrorResponse(
			c,
			"upstream_error",
			"Nexus returned status "+http.StatusText(resp.StatusCode)+": "+snippet,
			nil,
			http.StatusBadGateway,
		)
	}
}
