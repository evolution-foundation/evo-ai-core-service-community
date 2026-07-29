package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/middleware"
	"evo-ai-core-service/pkg/integration_credential/model"
	"evo-ai-core-service/pkg/integration_credential/service"

	"github.com/fernet/fernet-go"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// IntegrationCredentialHandler interface defines the contract for the vault handlers
type IntegrationCredentialHandler interface {
	RegisterRoutesMiddleware(router gin.IRouter)
	Create(c *gin.Context)
	GetByID(c *gin.Context)
	List(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
}

type integrationCredentialHandler struct {
	credentialService service.IntegrationCredentialService
	encryptionKey     string
}

func NewIntegrationCredentialHandler(credentialService service.IntegrationCredentialService, encryptionKey string) IntegrationCredentialHandler {
	return &integrationCredentialHandler{
		credentialService: credentialService,
		encryptionKey:     encryptionKey,
	}
}

// RegisterRoutesMiddleware registers the routes for the integration credential
// handler. The group sits at the top level, not under /agents: the api_key
// routes carry an ordering hazard against /agents/:id that this avoids.
func (h *integrationCredentialHandler) RegisterRoutesMiddleware(router gin.IRouter) {
	permissionMiddleware := middleware.GetGlobalPermissionMiddleware()

	credentials := router.Group("/integration-credentials")
	{
		credentials.GET("",
			permissionMiddleware.RequirePermission("ai_integration_credentials", "read"),
			h.List)
		credentials.GET("/",
			permissionMiddleware.RequirePermission("ai_integration_credentials", "read"),
			h.List)
		credentials.GET("/:id",
			permissionMiddleware.RequirePermission("ai_integration_credentials", "read"),
			h.GetByID)

		credentials.POST("",
			permissionMiddleware.RequirePermission("ai_integration_credentials", "create"),
			h.Create)
		credentials.POST("/",
			permissionMiddleware.RequirePermission("ai_integration_credentials", "create"),
			h.Create)

		credentials.PUT("/:id",
			permissionMiddleware.RequirePermission("ai_integration_credentials", "update"),
			h.Update)

		credentials.DELETE("/:id",
			permissionMiddleware.RequirePermission("ai_integration_credentials", "delete"),
			h.Delete)
	}
}

// encryptValue uses the same Fernet key the api_key handler does, shared with
// evo-ai-processor through ENCRYPTION_KEY so the runtime can decrypt what the
// screen stored.
func (h *integrationCredentialHandler) encryptValue(value string) (string, error) {
	fernetKey, err := fernet.DecodeKey(h.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("invalid encryption key: %w", err)
	}

	encrypted, err := fernet.EncryptAndSign([]byte(value), fernetKey)
	if err != nil {
		return "", fmt.Errorf("encryption failed: %w", err)
	}

	return string(encrypted), nil
}

// deriveHint reads the hint from the plaintext BEFORE encryption. A composite
// value hints on its sensitive component: hinting on the envelope would render
// JSON syntax, and hinting on the public half would mask the wrong component.
func deriveHint(value, valueFormat string) (string, error) {
	if valueFormat == model.ValueFormatComposite {
		return model.DeriveCompositeHint(value)
	}
	return model.DeriveValueHint(value), nil
}

// rejectOAuth guards the kind this story does not implement. Story 2.5 is what
// defines where an oauth row points and who keeps its state in sync; accepting
// one here would create a row with no semantics behind it.
func rejectOAuth(kind string) error {
	if model.NormalizeKind(kind) == model.KindOAuth {
		return fmt.Errorf("kind \"oauth\" is not supported yet: the vault stores static secrets, and oauth credentials enter by reference in a later story")
	}
	return nil
}

func (h *integrationCredentialHandler) Create(c *gin.Context) {
	var req *model.IntegrationCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	if err := rejectOAuth(req.Kind); err != nil {
		response.ErrorResponse(c, "ERR_INVALID_REQUEST", err.Error(), nil, http.StatusBadRequest)
		return
	}

	if req.Value == "" {
		response.ErrorResponse(c, "ERR_INVALID_REQUEST", "value is required for a static credential", nil, http.StatusBadRequest)
		return
	}

	valueFormat := model.NormalizeValueFormat(req.ValueFormat)

	hint, err := deriveHint(req.Value, valueFormat)
	if err != nil {
		response.ErrorResponse(c, "ERR_INVALID_REQUEST", err.Error(), nil, http.StatusBadRequest)
		return
	}

	encryptedValue, err := h.encryptValue(req.Value)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	credential := model.IntegrationCredential{
		Name:        req.Name,
		Provider:    req.Provider,
		Kind:        model.KindStatic,
		Scope:       model.NormalizeScope(req.Scope),
		ValueFormat: valueFormat,
		Value:       encryptedValue,
		ValueHint:   hint,
	}

	created, err := h.credentialService.Create(c.Request.Context(), credential)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, created.ToResponse(), "Integration credential created successfully", http.StatusCreated)
}

func (h *integrationCredentialHandler) GetByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	credential, err := h.credentialService.GetByID(c.Request.Context(), id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, credential.ToResponse(), "Integration credential retrieved successfully", http.StatusOK)
}

func (h *integrationCredentialHandler) List(c *gin.Context) {
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

	req := model.IntegrationCredentialListRequest{
		Page:     page,
		PageSize: pageSize,
		Active:   c.DefaultQuery("active", ""),
		Scope:    c.DefaultQuery("scope", ""),
		Kind:     c.DefaultQuery("kind", ""),
		Provider: c.DefaultQuery("provider", ""),
	}

	list, err := h.credentialService.List(c.Request.Context(), req)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.PaginatedResponse(c, list.Items, list.Page, list.PageSize, int(list.TotalItems), "Integration credentials retrieved successfully", http.StatusOK)
}

func (h *integrationCredentialHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	var req model.IntegrationCredentialUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	if err := rejectOAuth(req.Kind); err != nil {
		response.ErrorResponse(c, "ERR_INVALID_REQUEST", err.Error(), nil, http.StatusBadRequest)
		return
	}

	credential := &model.IntegrationCredential{
		Name:     req.Name,
		Provider: req.Provider,
	}

	// An omitted scope keeps the stored one (GORM skips zero-valued fields);
	// a provided one is normalized so no request can write an invalid scope.
	if req.Scope != "" {
		credential.Scope = model.NormalizeScope(req.Scope)
	}

	// An empty value means "keep the stored one": GORM's Updates skips
	// zero-valued struct fields, so Value, ValueFormat and ValueHint stay
	// untouched and the hint never desyncs from the secret.
	if req.Value != "" {
		valueFormat := model.NormalizeValueFormat(req.ValueFormat)

		hint, err := deriveHint(req.Value, valueFormat)
		if err != nil {
			response.ErrorResponse(c, "ERR_INVALID_REQUEST", err.Error(), nil, http.StatusBadRequest)
			return
		}

		encryptedValue, err := h.encryptValue(req.Value)
		if err != nil {
			code, message, httpCode := errors.HandleError(err)
			response.ErrorResponse(c, code, message, nil, httpCode)
			return
		}

		credential.ValueFormat = valueFormat
		credential.Value = encryptedValue
		credential.ValueHint = hint
	}

	updated, err := h.credentialService.Update(c.Request.Context(), credential, id)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, updated.ToResponse(), "Integration credential updated successfully", http.StatusOK)
}

func (h *integrationCredentialHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	if _, err := h.credentialService.Delete(c.Request.Context(), id); err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, nil, "Integration credential deleted successfully", http.StatusNoContent)
}
