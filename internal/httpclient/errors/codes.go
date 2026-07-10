package errors

import (
	"net/http"

	"evo-ai-core-service/internal/infra/postgres"
)

// Standard error codes
const (
	// 400 Bad Request
	ValidationError      = "VALIDATION_ERROR"
	InvalidInput         = "INVALID_INPUT"
	MissingRequiredField = "MISSING_REQUIRED_FIELD"
	BadRequest           = "BAD_REQUEST"

	// 401 Unauthorized
	Unauthorized = "UNAUTHORIZED"
	InvalidToken = "INVALID_TOKEN"
	TokenExpired = "TOKEN_EXPIRED"

	// 403 Forbidden
	Forbidden               = "FORBIDDEN"
	InsufficientPermissions = "INSUFFICIENT_PERMISSIONS"

	// 404 Not Found
	NotFound            = "NOT_FOUND"
	ResourceNotFound    = "RESOURCE_NOT_FOUND"
	AgentNotFound       = "AGENT_NOT_FOUND"
	FolderNotFound      = "FOLDER_NOT_FOUND"
	ApiKeyNotFound      = "API_KEY_NOT_FOUND"
	CustomToolNotFound  = "CUSTOM_TOOL_NOT_FOUND"
	McpServerNotFound   = "MCP_SERVER_NOT_FOUND"
	IntegrationNotFound = "INTEGRATION_NOT_FOUND"

	// 409 Conflict
	ResourceAlreadyExists = "RESOURCE_ALREADY_EXISTS"
	DuplicateEmail        = "DUPLICATE_EMAIL"
	Conflict              = "CONFLICT"

	// 422 Unprocessable Entity
	BusinessRuleViolation  = "BUSINESS_RULE_VIOLATION"
	InvalidStateTransition = "INVALID_STATE_TRANSITION"

	// 500 Internal Server Error
	InternalError        = "INTERNAL_ERROR"
	DatabaseError        = "DATABASE_ERROR"
	ExternalServiceError = "EXTERNAL_SERVICE_ERROR"
	TimeoutError         = "TIMEOUT_ERROR"
)

// ApiError represents a standardized API error
type ApiError struct {
	Code     string      `json:"code"`
	Message  string      `json:"message"`
	Details  interface{} `json:"details,omitempty"`
	HTTPCode int         `json:"http_code"`
	Err      error       `json:"-"`
}

// Error implements the error interface
func (e *ApiError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

// Unwrap implements the unwrap interface
func (e *ApiError) Unwrap() error {
	return e.Err
}

// New creates a new API error
func New(code string, message string, httpCode int) *ApiError {
	return &ApiError{
		Code:     code,
		Message:  message,
		HTTPCode: httpCode,
	}
}

// WithError wraps an existing error
func (e *ApiError) WithError(err error) *ApiError {
	e.Err = err
	if err != nil && e.Message == "" {
		e.Message = err.Error()
	}
	return e
}

// WithDetails adds details to the error
func (e *ApiError) WithDetails(details interface{}) *ApiError {
	e.Details = details
	return e
}

// Common error constructors
var (
	// 400 errors
	ErrValidation      = New(ValidationError, "Validation failed", http.StatusBadRequest)
	ErrInvalidInput    = New(InvalidInput, "Invalid input", http.StatusBadRequest)
	ErrMissingRequired = New(MissingRequiredField, "Missing required field", http.StatusBadRequest)
	ErrBadRequest      = New(BadRequest, "Bad request", http.StatusBadRequest)

	// 401 errors
	ErrUnauthorized = New(Unauthorized, "Unauthorized", http.StatusUnauthorized)
	ErrInvalidToken = New(InvalidToken, "Invalid token", http.StatusUnauthorized)
	ErrTokenExpired = New(TokenExpired, "Token expired", http.StatusUnauthorized)

	// 403 errors
	ErrForbidden     = New(Forbidden, "Access forbidden", http.StatusForbidden)
	ErrNoPermissions = New(InsufficientPermissions, "Insufficient permissions", http.StatusForbidden)

	// 404 errors
	ErrNotFound           = New(NotFound, "Resource not found", http.StatusNotFound)
	ErrAgentNotFound      = New(AgentNotFound, "Agent not found", http.StatusNotFound)
	ErrFolderNotFound     = New(FolderNotFound, "Folder not found", http.StatusNotFound)
	ErrApiKeyNotFound     = New(ApiKeyNotFound, "API key not found", http.StatusNotFound)
	ErrCustomToolNotFound = New(CustomToolNotFound, "Custom tool not found", http.StatusNotFound)
	ErrMcpServerNotFound  = New(McpServerNotFound, "MCP server not found", http.StatusNotFound)

	// 409 errors
	ErrAlreadyExists = New(ResourceAlreadyExists, "Resource already exists", http.StatusConflict)
	ErrConflict      = New(Conflict, "Resource conflict", http.StatusConflict)

	// 422 errors
	ErrBusinessRule = New(BusinessRuleViolation, "Business rule violation", http.StatusUnprocessableEntity)
	ErrInvalidState = New(InvalidStateTransition, "Invalid state transition", http.StatusUnprocessableEntity)

	// 500 errors
	ErrInternal        = New(InternalError, "Internal server error", http.StatusInternalServerError)
	ErrDatabase        = New(DatabaseError, "Database error", http.StatusInternalServerError)
	ErrExternalService = New(ExternalServiceError, "External service error", http.StatusInternalServerError)
	ErrTimeout         = New(TimeoutError, "Request timeout", http.StatusGatewayTimeout)
)

// HandleError converts various error types to standardized code, message, and HTTP status
func HandleError(err error) (code string, message string, httpCode int) {
	if err == nil {
		return "", "", http.StatusOK
	}

	// Check if it's already an ApiError
	if apiErr, ok := err.(*ApiError); ok {
		return apiErr.Code, apiErr.Message, apiErr.HTTPCode
	}

	// Errors mapped from the database layer (postgres.MapDBError) carry their own
	// code + HTTP status (duplicate key → 409, record not found → 404, etc.). Sem
	// este ramo eles caíam no default abaixo e viravam 500/INTERNAL_ERROR — foi o
	// motivo de "API key with this name already exists" retornar 500 em vez de 409.
	if dbErr, ok := err.(*postgres.Error); ok {
		return string(dbErr.Code), dbErr.Message, dbErr.HTTPCode
	}

	// Default to internal error
	return InternalError, err.Error(), http.StatusInternalServerError
}
