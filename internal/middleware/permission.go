package middleware

import (
	"context"
	"fmt"
	"net/http"

	"evo-ai-core-service/internal/httpclient/response"
	"evo-ai-core-service/internal/services"
	"evo-ai-core-service/internal/utils/contextutils"

	"github.com/gin-gonic/gin"
)

// Singleton global para o middleware de permissão
var globalPermissionMiddleware PermissionMiddleware

// InitializePermissionMiddleware inicializa o middleware global
func InitializePermissionMiddleware(evoAuthBaseURL string) {
	globalPermissionMiddleware = NewPermissionMiddleware(evoAuthBaseURL)
}

// SetGlobalPermissionMiddleware replaces the global middleware and returns a
// function restoring the previous one. Tests use it to exercise the authorization
// PATH — the gates that live inside handlers cannot be reached otherwise, and a
// gate nobody calls is exactly the defect class this guards against.
func SetGlobalPermissionMiddleware(m PermissionMiddleware) func() {
	previous := globalPermissionMiddleware
	globalPermissionMiddleware = m
	return func() { globalPermissionMiddleware = previous }
}

// GetGlobalPermissionMiddleware retorna o middleware global
func GetGlobalPermissionMiddleware() PermissionMiddleware {
	if globalPermissionMiddleware == nil {
		panic("Permission middleware not initialized. Call InitializePermissionMiddleware first.")
	}
	return globalPermissionMiddleware
}

// PermissionMiddleware interface para validação de permissões
type PermissionMiddleware interface {
	RequirePermission(resource, action string) gin.HandlerFunc
	CheckPermission(authToken, permissionKey string) (bool, error)
	CheckPermissionWithType(authToken, permissionKey, tokenType string) (bool, error)
	// HasPermission checks a permission from INSIDE a handler, for gates that
	// depend on the request body and therefore cannot live on the route.
	HasPermission(c *gin.Context, resource, action string) (bool, error)
}

// InstallationScopePermission is the permission that governs writing at the
// INSTALLATION level: a credential that every account inherits.
//
// It has to be checked inside the handler, not on the route: the scope arrives
// in the body, so a route-level middleware cannot tell an account write from an
// installation write. It used to be enforced only in the browser, which meant a
// plain account admin could create or edit the installation default with a
// curl (EVO-2250 review, ALTO 5).
const (
	InstallationScopeResource = "installation_configs"
	InstallationScopeAction   = "manage"
)

// RequireInstallationScope answers whether the caller may write at the
// installation level, writing the 401/403 response itself when it may not.
//
// Returns true when the caller is allowed to proceed. Any failure to reach the
// auth service is a denial, never a pass: a credential that every account
// inherits is not something to grant on a network error.
func RequireInstallationScope(c *gin.Context) bool {
	// No middleware means no way to authorize, which is a denial rather than a
	// panic: an uninitialized gate must not become an open door.
	if globalPermissionMiddleware == nil {
		response.ErrorResponse(c, "ERR_INTERNAL_SERVER", "Unable to validate user permissions", nil, http.StatusInternalServerError)
		c.Abort()
		return false
	}

	allowed, err := globalPermissionMiddleware.HasPermission(c, InstallationScopeResource, InstallationScopeAction)
	if err != nil {
		response.ErrorResponse(c, "ERR_INTERNAL_SERVER", "Unable to validate user permissions", nil, http.StatusInternalServerError)
		c.Abort()
		return false
	}

	if !allowed {
		response.ErrorResponse(
			c,
			"ERR_FORBIDDEN",
			"installation_configs.manage is required to write a credential at the installation scope",
			nil,
			http.StatusForbidden,
		)
		c.Abort()
		return false
	}

	return true
}

type permissionMiddleware struct {
	authService services.EvoAuthService
}

// NewPermissionMiddleware cria uma nova instância do middleware de permissões
// Delegando toda lógica para EvoAuthService para consistência
func NewPermissionMiddleware(evoAuthBaseURL string) PermissionMiddleware {
	return &permissionMiddleware{
		authService: services.NewEvoAuthService(evoAuthBaseURL),
	}
}

// RequirePermission cria um middleware que valida se o usuário tem a permissão específica
func (p *permissionMiddleware) RequirePermission(resource, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Construir chave de permissão
		permissionKey := fmt.Sprintf("%s.%s", resource, action)

		// Get token type from context (set by EvoAuthMiddleware)
		tokenType, err := contextutils.GetTokenType(c.Request.Context())
		authToken, err := contextutils.GetToken(c.Request.Context())
		if authToken == "" {
			response.ErrorResponse(c, "ERR_UNAUTHORIZED", "Token must be provided in header", nil, http.StatusUnauthorized)
			c.Abort()
			return
		}

		hasPermission, err := p.CheckPermissionWithType(authToken, permissionKey, tokenType)

		if err != nil {
			fmt.Printf("Permission: Error checking permission %s: %v\n", permissionKey, err)
			response.ErrorResponse(c, "ERR_INTERNAL_SERVER", "Unable to validate user permissions", nil, http.StatusInternalServerError)
			c.Abort()
			return
		}

		fmt.Printf("Permission: Has permission %s: %v\n", permissionKey, hasPermission)

		if !hasPermission {
			response.ErrorResponse(c, "ERR_FORBIDDEN", "Insufficient permissions", nil, http.StatusForbidden)
			c.Abort()
			return
		}

		fmt.Printf("Permission: Access granted for permission %s\n", permissionKey)
		c.Next()
	}
}

// HasPermission checks a permission for the caller of the current request.
// Handlers use it for gates that depend on the request body, which a
// route-level middleware cannot see.
func (p *permissionMiddleware) HasPermission(c *gin.Context, resource, action string) (bool, error) {
	permissionKey := fmt.Sprintf("%s.%s", resource, action)

	tokenType, _ := contextutils.GetTokenType(c.Request.Context())
	authToken, _ := contextutils.GetToken(c.Request.Context())
	if authToken == "" {
		return false, nil
	}

	return p.CheckPermissionWithType(authToken, permissionKey, tokenType)
}

// CheckPermission delegates to EvoAuthService for unified permission handling
func (p *permissionMiddleware) CheckPermission(authToken, permissionKey string) (bool, error) {
	return p.CheckPermissionWithType(authToken, permissionKey, "bearer")
}

// CheckPermissionWithType delegates to EvoAuthService for unified permission handling with specific token type
func (p *permissionMiddleware) CheckPermissionWithType(authToken, permissionKey, tokenType string) (bool, error) {
	return p.authService.CheckPermission(context.Background(), authToken, permissionKey, tokenType)
}
