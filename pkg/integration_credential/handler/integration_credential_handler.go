package handler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

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
	MigrationState(c *gin.Context)
	Update(c *gin.Context)
	Delete(c *gin.Context)
}

// OAuthReconciler keeps the vault's oauth rows in step with the store that owns
// the tokens, and decorates them with the state read live from it.
type OAuthReconciler interface {
	Run(ctx context.Context) error
	Decorate(ctx context.Context, rows []model.IntegrationCredential, now time.Time) ([]*model.IntegrationCredentialResponse, error)
}

// ReferenceReporter aggregates, for a whole page, which consumers point at each
// vault credential (story 2.4 AC10).
type ReferenceReporter interface {
	Build(ctx context.Context) (service.ReferenceIndex, error)
}

type integrationCredentialHandler struct {
	credentialService service.IntegrationCredentialService
	encryptionKey     string
	oauthSync         OAuthReconciler
	migrationState    MigrationReporter
	references        ReferenceReporter
}

// MigrationReporter answers, per consumer, whether the inline fallback can be
// retired (story 2.7).
type MigrationReporter interface {
	Retired(ctx context.Context) (map[string]bool, error)
}

func NewIntegrationCredentialHandler(
	credentialService service.IntegrationCredentialService,
	encryptionKey string,
	oauthSync OAuthReconciler,
	migrationState MigrationReporter,
	references ReferenceReporter,
) IntegrationCredentialHandler {
	return &integrationCredentialHandler{
		credentialService: credentialService,
		encryptionKey:     encryptionKey,
		oauthSync:         oauthSync,
		migrationState:    migrationState,
		references:        references,
	}
}

// attachReferences fills `referenced_by` on every row of a page. Aggregation
// failure does not fail the request: the field stays absent and the screen
// falls back to not showing consumers.
func (h *integrationCredentialHandler) attachReferences(c *gin.Context, items []model.IntegrationCredentialResponse) []model.IntegrationCredentialResponse {
	if h.references == nil || len(items) == 0 {
		return items
	}

	index, err := h.references.Build(c.Request.Context())
	if err != nil {
		log.Printf("integration credentials: reference aggregation failed: %v", err)
		return items
	}

	for i := range items {
		items[i].ReferencedBy = index.For(items[i].ID)
	}

	return items
}

// MigrationState reports which consumers may retire their inline secret field.
// A read failure answers "nothing retired" with a 200: failing the request
// would leave the form unable to decide at all.
func (h *integrationCredentialHandler) MigrationState(c *gin.Context) {
	if h.migrationState == nil {
		response.SuccessResponse(c, gin.H{"retired": map[string]bool{}}, "Migration state retrieved successfully", http.StatusOK)
		return
	}

	retired, err := h.migrationState.Retired(c.Request.Context())
	if err != nil {
		log.Printf("integration credentials: migration state read failed: %v", err)
	}

	response.SuccessResponse(c, gin.H{"retired": retired}, "Migration state retrieved successfully", http.StatusOK)
}

// decorateOAuthRows fills the mirrored connection state on oauth rows. Static
// rows pass through untouched, and the mirrored fields are never persisted.
func (h *integrationCredentialHandler) decorateOAuthRows(c *gin.Context, items []model.IntegrationCredentialResponse) ([]model.IntegrationCredentialResponse, error) {
	if h.oauthSync == nil {
		return items, nil
	}

	oauthRows := make([]model.IntegrationCredential, 0)
	for _, item := range items {
		if item.Kind == model.KindOAuth {
			oauthRows = append(oauthRows, model.IntegrationCredential{
				ID:         item.ID,
				Name:       item.Name,
				Provider:   item.Provider,
				Kind:       item.Kind,
				Scope:      item.Scope,
				OwnerStore: item.OwnerStore,
				OwnerRef:   item.OwnerRef,
				IsActive:   item.IsActive,
				CreatedAt:  item.CreatedAt,
				UpdatedAt:  item.UpdatedAt,
			})
		}
	}

	if len(oauthRows) == 0 {
		return items, nil
	}

	decorated, err := h.oauthSync.Decorate(c.Request.Context(), oauthRows, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	byID := make(map[uuid.UUID]*model.IntegrationCredentialResponse, len(decorated))
	for _, row := range decorated {
		byID[row.ID] = row
	}

	result := make([]model.IntegrationCredentialResponse, 0, len(items))
	for _, item := range items {
		if enriched, ok := byID[item.ID]; ok {
			result = append(result, *enriched)
			continue
		}
		result = append(result, item)
	}

	return result, nil
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
		// Registered BEFORE /:id so the literal path is not captured by the
		// parameterized one.
		credentials.GET("/migration-state",
			permissionMiddleware.RequirePermission("ai_integration_credentials", "read"),
			h.MigrationState)

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

// authorizeScopeWrite guards a write that touches the installation scope,
// either by requesting it or by targeting a credential already stored with it.
// Returns false when the response has already been written.
func (h *integrationCredentialHandler) authorizeScopeWrite(c *gin.Context, requestedScope string, id uuid.UUID) bool {
	touchesInstallation := requestedScope == model.ScopeInstallation

	if !touchesInstallation {
		// A failed lookup demands the privilege, same as the api_key handler.
		stored, err := h.credentialService.GetByID(c.Request.Context(), id)
		switch {
		case err != nil:
			touchesInstallation = true
		case stored != nil:
			touchesInstallation = stored.Scope == model.ScopeInstallation
		}
	}

	if !touchesInstallation {
		return true
	}

	return middleware.RequireInstallationScope(c)
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

	// The installation scope is a separate privilege: this credential becomes
	// the default every account inherits. Checked here and not on the route
	// because the scope travels in the body .
	scope := model.NormalizeScope(req.Scope)
	if scope == model.ScopeInstallation && !middleware.RequireInstallationScope(c) {
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
		Scope:       scope,
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

	single := h.attachReferences(c, []model.IntegrationCredentialResponse{*credential.ToResponse()})

	response.SuccessResponse(c, single[0], "Integration credential retrieved successfully", http.StatusOK)
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

	// The oauth rows are reconciled on read: a connection made through the
	// existing OAuth flow (which lives in the processor and is deliberately
	// untouched) shows up on the next listing. A sync failure is not fatal, the
	// static credentials still list.
	if h.oauthSync != nil {
		if err := h.oauthSync.Run(c.Request.Context()); err != nil {
			log.Printf("integration credentials: oauth sync failed: %v", err)
		}
	}

	list, err := h.credentialService.List(c.Request.Context(), req)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	items, err := h.decorateOAuthRows(c, list.Items)
	if err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	items = h.attachReferences(c, items)

	response.PaginatedResponse(c, items, list.Page, list.PageSize, int(list.TotalItems), "Integration credentials retrieved successfully", http.StatusOK)
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

	// Promoting into the installation scope AND editing a credential already
	// stored with it both need the privilege .
	if !h.authorizeScopeWrite(c, credential.Scope, id) {
		return
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

	updated, err := h.credentialService.Update(c.Request.Context(), credential, req.IsActive, id)
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

	// Deleting the installation default is a write to it.
	if !h.authorizeScopeWrite(c, "", id) {
		return
	}

	if _, err := h.credentialService.Delete(c.Request.Context(), id); err != nil {
		code, message, httpCode := errors.HandleError(err)
		response.ErrorResponse(c, code, message, nil, httpCode)
		return
	}

	response.SuccessResponse(c, nil, "Integration credential deleted successfully", http.StatusNoContent)
}
