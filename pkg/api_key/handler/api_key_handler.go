package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/pkg/api_key/model"
	"evo-ai-core-service/pkg/api_key/service"

	"github.com/fernet/fernet-go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ApiKeyHandler interface defines the contract for api key handlers
type ApiKeyHandler interface {
	RegisterRoutesMiddleware(router gin.IRouter)
	Create(c *gin.Context)
	GetByID(c *gin.Context)
	List(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
	GetModels(c *gin.Context)
}

// apiKeyHandler implements the ApiKeyHandler interface
type apiKeyHandler struct {
	apiKeyService service.ApiKeyService
	encryptionKey string
}

// NewApiKeyHandler creates a new api key handler
func NewApiKeyHandler(apiKeyService service.ApiKeyService, encryptionKey string) ApiKeyHandler {
	return &apiKeyHandler{
		apiKeyService: apiKeyService,
		encryptionKey: encryptionKey,
	}
}

// RegisterRoutesMiddleware registers the routes for the api key handler with middleware
func (h *apiKeyHandler) RegisterRoutesMiddleware(router gin.IRouter) {
	// Usar middleware global de permissão
	permissionMiddleware := middleware.GetGlobalPermissionMiddleware()

	// Routes for API keys - must be registered BEFORE agent routes
	// to ensure /agents/apikeys is captured before /agents/:id
	apiKeys := router.Group("/agents/apikeys")
	{
		// Read permissions
		apiKeys.GET("",
			permissionMiddleware.RequirePermission("ai_api_keys", "read"),
			h.List)
		apiKeys.GET("/",
			permissionMiddleware.RequirePermission("ai_api_keys", "read"),
			h.List)
		apiKeys.GET("/:id",
			permissionMiddleware.RequirePermission("ai_api_keys", "read"),
			h.GetByID)
		apiKeys.GET("/:id/models",
			permissionMiddleware.RequirePermission("ai_api_keys", "read"),
			h.GetModels)

		// Create permissions
		apiKeys.POST("",
			permissionMiddleware.RequirePermission("ai_api_keys", "create"),
			h.Create)
		apiKeys.POST("/",
			permissionMiddleware.RequirePermission("ai_api_keys", "create"),
			h.Create)

		// Update permissions
		apiKeys.PUT("/:id",
			permissionMiddleware.RequirePermission("ai_api_keys", "update"),
			h.Update)

		// Delete permissions
		apiKeys.DELETE("/:id",
			permissionMiddleware.RequirePermission("ai_api_keys", "delete"),
			h.Delete)
	}
}

func (h *apiKeyHandler) decryptKey(encrypted string) (string, error) {
	fernetKey, err := fernet.DecodeKey(h.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("invalid encryption key: %w", err)
	}

	plain := fernet.VerifyAndDecrypt([]byte(encrypted), 0, []*fernet.Key{fernetKey})
	if plain == nil {
		return "", fmt.Errorf("failed to decrypt api key")
	}

	return string(plain), nil
}

func (h *apiKeyHandler) encryptKey(key string) (string, error) {
	// Use Fernet encryption with a fixed key from environment
	// This key is shared with evo-ai-processor for decryption
	fernetKey, err := fernet.DecodeKey(h.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("invalid encryption key: %w", err)
	}

	encryptedKey, err := fernet.EncryptAndSign([]byte(key), fernetKey)
	if err != nil {
		return "", fmt.Errorf("encryption failed: %w", err)
	}

	return string(encryptedKey), nil
}

// Create handles the create api key request
func (h *apiKeyHandler) Create(c *gin.Context) {
	var req *model.ApiKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	// Validate that at least one key was provided
	actualKey := req.GetKey()
	if actualKey == "" {
		code, message, httpCode := errors.HandleError(fmt.Errorf("key or key_value is required"))
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	encryptedKey, err := h.encryptKey(actualKey)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	apiKey := model.ApiKey{
		Name:     req.Name,
		Provider: req.Provider,
		Key:      encryptedKey,
	}

	createdApiKey, err := h.apiKeyService.Create(c.Request.Context(), apiKey)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, createdApiKey.ToResponse(), "API key created successfully", http.StatusCreated)
}

// GetByID handles the get api key by id request
func (h *apiKeyHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	apiKey, err := h.apiKeyService.GetByID(c.Request.Context(), id)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, apiKey.ToResponse(), "API key retrieved successfully", http.StatusOK)
}

// List handles the list api keys request
func (h *apiKeyHandler) List(c *gin.Context) {
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

	active := c.DefaultQuery("active", "")

	var req model.ApiKeyListRequest
	req.Page = page
	req.PageSize = pageSize
	req.Active = active

	listApiKeys, err := h.apiKeyService.List(c.Request.Context(), req)

	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.PaginatedResponse(c, listApiKeys.Items, listApiKeys.Page, listApiKeys.PageSize, int(listApiKeys.TotalItems), "API keys retrieved successfully", http.StatusOK)
}

// Update handles the update api key request
func (h *apiKeyHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	var req model.ApiKeyUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	// Validate that at least one key was provided
	actualKey := req.GetKey()
	if actualKey == "" {
		code, message, httpCode := errors.HandleError(fmt.Errorf("key or key_value is required"))
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	encryptedKey, err := h.encryptKey(actualKey)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	apiKey := &model.ApiKey{
		Name:     req.Name,
		Provider: req.Provider,
		Key:      encryptedKey,
	}

	updatedApiKey, err := h.apiKeyService.Update(c.Request.Context(), apiKey, id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, updatedApiKey.ToResponse(), "API key updated successfully", http.StatusOK)
}

// GetModels returns the list of models available for the provider associated
// with the given API key, by calling the provider's models endpoint with the
// decrypted key. The caller passes only the key ID — the plaintext key never
// leaves the server.
func (h *apiKeyHandler) GetModels(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	apiKey, err := h.apiKeyService.GetByID(c.Request.Context(), id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	if !service.ProviderSupportsDynamicModels(apiKey.Provider) {
		response.SuccessResponse(c, gin.H{
			"provider":  apiKey.Provider,
			"supported": false,
			"models":    []service.ModelInfo{},
		}, "Provider does not support dynamic model listing", http.StatusOK)
		return
	}

	plainKey, err := h.decryptKey(apiKey.Key)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	models, err := service.FetchProviderModels(c.Request.Context(), apiKey.Provider, plainKey)
	if err != nil {
		code, message, httpCode := errors.HandleError(fmt.Errorf("failed to fetch models from provider: %w", err))
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, gin.H{
		"provider":  apiKey.Provider,
		"supported": true,
		"models":    models,
	}, "Models retrieved successfully", http.StatusOK)
}

// Delete handles the delete api key request
func (h *apiKeyHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	_, err = h.apiKeyService.Delete(c.Request.Context(), id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, nil, "API key deleted successfully", http.StatusNoContent)
}
