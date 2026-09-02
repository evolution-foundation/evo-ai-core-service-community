package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"

	"evo-ai-core-service/internal/httpclient"
	"evo-ai-core-service/internal/types"
)

// Custom errors following Evolution pattern
type AuthenticationError struct {
	Message string
}

func (e *AuthenticationError) Error() string {
	return fmt.Sprintf("Authentication error: %s", e.Message)
}

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("Validation error: %s", e.Message)
}

type NetworkError struct {
	Message string
}

func (e *NetworkError) Error() string {
	return fmt.Sprintf("Network error: %s", e.Message)
}

type ServiceUnavailableError struct {
	Message string
}

func (e *ServiceUnavailableError) Error() string {
	return fmt.Sprintf("Service unavailable: %s", e.Message)
}

// NotImplementedError is returned only when evo-auth answers 404 for an
// endpoint, meaning the feature is not deployed there (as opposed to an outage).
type NotImplementedError struct {
	Message string
}

func (e *NotImplementedError) Error() string {
	return fmt.Sprintf("Not implemented: %s", e.Message)
}

// AllowMissingPermissionEndpointEnv opts in to granting permissions when
// evo-auth has no permission endpoint (404). Any other failure always denies.
const AllowMissingPermissionEndpointEnv = "EVO_AUTH_ALLOW_MISSING_PERMISSION_ENDPOINT"

// EvoAuthService interface defines all authentication and authorization operations
type EvoAuthService interface {
	// Authentication methods
	ValidateToken(token, tokenType string) (*types.EvoAuthValidateTokenData, error)
	BuildHeaders(token, tokenType string) (map[string]string, error)

	// Permission management methods
	CheckPermission(ctx context.Context, authToken, permissionKey, tokenType string) (bool, error)
	CheckAccountPermission(ctx context.Context, userID, accountID, permissionKey string, authToken, tokenType string) (bool, error)
	CheckUserPermission(ctx context.Context, userID, permissionKey string, authToken, tokenType string) (bool, error)
}

type evoAuthService struct {
	baseURL string
}

// NewEvoAuthService creates a new instance of EvoAuthService
func NewEvoAuthService(baseURL string) EvoAuthService {
	return &evoAuthService{
		baseURL: baseURL,
	}
}

// ============================================================================
// Authentication Methods
// ============================================================================

// ValidateToken validates token with Evo Auth API - Primary authentication method
func (s *evoAuthService) ValidateToken(token, tokenType string) (*types.EvoAuthValidateTokenData, error) {
	headers, err := s.BuildHeaders(token, tokenType)
	if err != nil {
		return nil, err
	}

	fmt.Printf("EvoAuth: Validating %s token at %s/api/v1/auth/validate\n", tokenType, s.baseURL)

	response, err := s.doPost("/api/v1/auth/validate", map[string]interface{}{}, headers)
	if err != nil {
		var networkErr *NetworkError
		var notImplementedErr *NotImplementedError
		if errors.As(err, &networkErr) || errors.As(err, &notImplementedErr) {
			return nil, &ServiceUnavailableError{Message: "Authentication service unavailable"}
		}
		return nil, err
	}

	// Parse response
	dataMap, ok := response["data"].(map[string]interface{})
	if !ok {
		return nil, &ValidationError{Message: "Invalid response format from auth service"}
	}

	// Convert to JSON and back to struct for proper type conversion
	dataJSON, err := json.Marshal(dataMap)
	if err != nil {
		return nil, &ValidationError{Message: "Failed to serialize response data"}
	}

	var tokenData types.EvoAuthValidateTokenData
	if err := json.Unmarshal(dataJSON, &tokenData); err != nil {
		return nil, &ValidationError{Message: "Failed to parse token data"}
	}

	fmt.Printf("EvoAuth: Successfully validated token for user %s with %d accounts\n", tokenData.User.Email, len(tokenData.Accounts))
	return &tokenData, nil
}

// BuildHeaders builds HTTP headers based on token type
func (s *evoAuthService) BuildHeaders(token, tokenType string) (map[string]string, error) {
	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
	}

	switch tokenType {
	case "bearer":
		headers["Authorization"] = fmt.Sprintf("Bearer %s", token)
	case "api_access_token":
		headers["api_access_token"] = token
	default:
		return nil, fmt.Errorf("invalid token type: %s", tokenType)
	}

	return headers, nil
}

// ============================================================================
// Permission Management Methods
// ============================================================================

// CheckPermission checks if authenticated user has specific permission
func (s *evoAuthService) CheckPermission(ctx context.Context, authToken, permissionKey, tokenType string) (bool, error) {
	headers, err := s.BuildHeaders(authToken, tokenType)
	if err != nil {
		return false, err
	}

	payload := map[string]interface{}{
		"permission_key": permissionKey,
	}

	response, err := s.doPost("/api/v1/permissions/check", payload, headers)
	if err != nil {
		return permissionVerdictOnError(permissionKey, err)
	}

	return permissionVerdict(permissionKey, response)
}

// CheckAccountPermission checks account-scoped permission for user
func (s *evoAuthService) CheckAccountPermission(ctx context.Context, userID, accountID, permissionKey string, authToken, tokenType string) (bool, error) {
	headers, err := s.BuildHeaders(authToken, tokenType)
	if err != nil {
		return false, err
	}

	payload := map[string]interface{}{
		"permission_key": permissionKey,
	}

	response, err := s.doPost(fmt.Sprintf("/api/v1/accounts/%s/users/%s/check_permission", accountID, userID), payload, headers)
	if err != nil {
		return permissionVerdictOnError(permissionKey, err)
	}

	return permissionVerdict(permissionKey, response)
}

// CheckUserPermission checks global user permission
func (s *evoAuthService) CheckUserPermission(ctx context.Context, userID, permissionKey string, authToken, tokenType string) (bool, error) {
	headers, err := s.BuildHeaders(authToken, tokenType)
	if err != nil {
		return false, err
	}

	payload := map[string]interface{}{
		"permission_key": permissionKey,
	}

	response, err := s.doPost("/api/v1/users/check_permission", payload, headers)
	if err != nil {
		return permissionVerdictOnError(permissionKey, err)
	}

	return permissionVerdict(permissionKey, response)
}

// permissionVerdictOnError fails closed: an unreachable, broken or rejecting
// auth service never grants. The single opt-in covers a 404 (endpoint absent).
func permissionVerdictOnError(permissionKey string, err error) (bool, error) {
	var notImplemented *NotImplementedError
	if errors.As(err, &notImplemented) {
		if allowMissingPermissionEndpoint() {
			fmt.Printf("EvoAuth: permission endpoint not implemented, granting %s because %s=true\n", permissionKey, AllowMissingPermissionEndpointEnv)
			return true, nil
		}
		fmt.Printf("EvoAuth: permission endpoint not implemented, denying %s (set %s=true to allow)\n", permissionKey, AllowMissingPermissionEndpointEnv)
		return false, err
	}

	fmt.Printf("EvoAuth: permission check for %s failed, denying: %v\n", permissionKey, err)
	return false, err
}

func permissionVerdict(permissionKey string, response map[string]interface{}) (bool, error) {
	data, ok := response["data"].(map[string]interface{})
	if !ok {
		return false, fmt.Errorf("invalid permission response for %s: missing data object", permissionKey)
	}

	hasPermission, _ := data["has_permission"].(bool)
	fmt.Printf("EvoAuth: permission check for %s: %v\n", permissionKey, hasPermission)
	return hasPermission, nil
}

func allowMissingPermissionEndpoint() bool {
	return os.Getenv(AllowMissingPermissionEndpointEnv) == "true"
}

// ============================================================================
// Private HTTP Methods
// ============================================================================

// doGet executes GET request to evo-auth-service using httpclient helpers
func (s *evoAuthService) doGet(endpoint string, headers map[string]string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s%s", s.baseURL, endpoint)

	// Use httpclient helper with flexible status code handling
	type Response map[string]interface{}

	// Try with 200 OK first
	result, err := httpclient.DoGetJSON[Response](
		context.Background(),
		url,
		headers,
		http.StatusOK,
	)

	if err != nil {
		return nil, classifyRequestError(err)
	}

	if result == nil {
		return nil, &ValidationError{Message: "Empty response from server"}
	}

	return *result, nil
}

// doPost executes POST request to evo-auth-service using httpclient helpers
func (s *evoAuthService) doPost(endpoint string, payload map[string]interface{}, headers map[string]string) (map[string]interface{}, error) {
	url := fmt.Sprintf("%s%s", s.baseURL, endpoint)

	// Use httpclient helper
	type Response map[string]interface{}

	result, err := httpclient.DoPostJSON[Response](
		context.Background(),
		url,
		payload,
		headers,
		http.StatusOK,
	)

	if err != nil {
		return nil, classifyRequestError(err)
	}

	if result == nil {
		return nil, &ValidationError{Message: "Empty response from server"}
	}

	return *result, nil
}

// classifyRequestError keeps transport failures and unexpected statuses as
// NetworkError; only a 404 becomes NotImplementedError and only a 401 an
// AuthenticationError.
func classifyRequestError(err error) error {
	var statusErr *httpclient.StatusError
	if errors.As(err, &statusErr) {
		switch statusErr.Code {
		case http.StatusNotFound:
			return &NotImplementedError{Message: "Endpoint not found"}
		case http.StatusUnauthorized:
			return &AuthenticationError{Message: "Invalid or expired token"}
		}
	}
	return &NetworkError{Message: fmt.Sprintf("Request failed: %v", err)}
}

// ============================================================================
// Global singleton instance
// ============================================================================

var globalEvoAuthService EvoAuthService

// InitializeEvoAuthService initializes the global service instance
func InitializeEvoAuthService(baseURL string) {
	globalEvoAuthService = NewEvoAuthService(baseURL)
	fmt.Printf("Global EvoAuthService initialized with base URL: %s\n", baseURL)
}

// GetEvoAuthService returns the global service instance
func GetEvoAuthService() EvoAuthService {
	if globalEvoAuthService == nil {
		panic("EvoAuthService not initialized. Call InitializeEvoAuthService first.")
	}
	return globalEvoAuthService
}
