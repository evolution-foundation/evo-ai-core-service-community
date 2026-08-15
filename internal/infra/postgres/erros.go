package postgres

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// ErrorCode represents a unique error code for the application
type ErrorCode string

// Common error codes
const (
	ERR_RECORD_NOT_FOUND           ErrorCode = "ERR_RECORD_NOT_FOUND"
	ERR_INVALID_REQUEST            ErrorCode = "ERR_INVALID_REQUEST"
	ERR_TIMEOUT_EXCEEDED           ErrorCode = "ERR_TIMEOUT_EXCEEDED"
	ERR_REQUEST_CANCELED           ErrorCode = "ERR_REQUEST_CANCELED"
	ERR_DUPLICATE_KEY_VIOLATION    ErrorCode = "ERR_DUPLICATE_KEY_VIOLATION"
	ERR_FOREIGN_KEY_VIOLATION      ErrorCode = "ERR_FOREIGN_KEY_VIOLATION"
	ERR_NOT_NULL_VIOLATION         ErrorCode = "ERR_NOT_NULL_VIOLATION"
	ERR_CHECK_CONSTRAINT_VIOLATION ErrorCode = "ERR_CHECK_CONSTRAINT_VIOLATION"
	ERR_VALUE_TOO_LONG             ErrorCode = "ERR_VALUE_TOO_LONG"
	ERR_UNDEFINED_COLUMN           ErrorCode = "ERR_UNDEFINED_COLUMN"
	ERR_UNDEFINED_TABLE            ErrorCode = "ERR_UNDEFINED_TABLE"
	ERR_SQL_SYNTAX_ERROR           ErrorCode = "ERR_SQL_SYNTAX_ERROR"
	ERR_DATATYPE_MISMATCH          ErrorCode = "ERR_DATATYPE_MISMATCH"
	ERR_SERIALIZATION_FAILURE      ErrorCode = "ERR_SERIALIZATION_FAILURE"
	ERR_DATABASE_ERROR             ErrorCode = "ERR_DATABASE_ERROR"
	ERR_UNEXPECTED_ERROR           ErrorCode = "ERR_UNEXPECTED_ERROR"
	ERR_RATE_LIMIT_EXCEEDED        ErrorCode = "ERR_RATE_LIMIT_EXCEEDED"
)

type Error struct {
	Code     ErrorCode
	Message  string
	HTTPCode int
	Err      error
}

type CustomErrorMessage struct {
	Code    string
	Message string
}

func (e *Error) Error() string {
	return e.Message
}

func (e *Error) Unwrap() error {
	return e.Err
}

func MapDBError(err error, customMessages []CustomErrorMessage) error {
	if err == nil {
		return nil
	}

	customMessageMap := make(map[string]string)
	for _, msg := range customMessages {
		customMessageMap[msg.Code] = msg.Message
	}

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return createError(ERR_RECORD_NOT_FOUND, "Record not found", http.StatusNotFound, err, customMessageMap)
	case errors.Is(err, gorm.ErrInvalidData),
		errors.Is(err, gorm.ErrPrimaryKeyRequired),
		errors.Is(err, gorm.ErrModelValueRequired),
		errors.Is(err, gorm.ErrEmptySlice):
		return createError(ERR_INVALID_REQUEST, "Invalid request", http.StatusBadRequest, err, customMessageMap)
	case errors.Is(err, context.DeadlineExceeded):
		return createError(ERR_TIMEOUT_EXCEEDED, "Timeout exceeded", http.StatusGatewayTimeout, err, customMessageMap)
	case errors.Is(err, context.Canceled):
		return createError(ERR_REQUEST_CANCELED, "Request canceled", http.StatusRequestTimeout, err, customMessageMap)
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // duplicate key
			return createError(ERR_DUPLICATE_KEY_VIOLATION, "Duplicate key violation", http.StatusConflict, err, customMessageMap)
		case "23503": // foreign key
			return createError(ERR_FOREIGN_KEY_VIOLATION, "Foreign key violation", http.StatusBadRequest, err, customMessageMap)
		case "23502": // not null
			return createError(ERR_NOT_NULL_VIOLATION, "Not null violation", http.StatusBadRequest, err, customMessageMap)
		case "23514": // check
			return createError(ERR_CHECK_CONSTRAINT_VIOLATION, "Check constraint violation", http.StatusBadRequest, err, customMessageMap)
		case "22001": // string truncation
			return createError(ERR_VALUE_TOO_LONG, "Value too long", http.StatusBadRequest, err, customMessageMap)
		case "42703": // undefined column
			return createSchemaError(ERR_UNDEFINED_COLUMN, "Undefined column", pgErr, err, customMessageMap)
		case "42P01": // undefined table
			return createSchemaError(ERR_UNDEFINED_TABLE, "Undefined table", pgErr, err, customMessageMap)
		case "42601": // syntax error
			return createError(ERR_SQL_SYNTAX_ERROR, "SQL syntax error", http.StatusInternalServerError, err, customMessageMap)
		case "42804": // datatype mismatch
			return createError(ERR_DATATYPE_MISMATCH, "Datatype mismatch", http.StatusBadRequest, err, customMessageMap)
		case "40001": // serialization failure
			return createError(ERR_SERIALIZATION_FAILURE, "Serialization failure", http.StatusConflict, err, customMessageMap)
		default:
			return createError(ERR_DATABASE_ERROR, "Database error", http.StatusInternalServerError, err, customMessageMap)
		}
	}

	// generic error
	return createError(ERR_UNEXPECTED_ERROR, "Unexpected error", http.StatusInternalServerError, err, customMessageMap)
}

// Schema errors (undefined column/table) mean the running binary is ahead of the
// applied migrations. The Postgres message names only the identifier — e.g.
// `column "base_url" of relation "evo_core_api_keys" does not exist` — never row
// data, so it is safe to surface and it is what turns a blind "Undefined column"
// into an actionable one. Logged as well: this was invisible in the pod log.
func createSchemaError(code ErrorCode, defaultMessage string, pgErr *pgconn.PgError, err error, customMessages map[string]string) error {
	log.Printf("postgres schema error %s (SQLSTATE %s): %s", code, pgErr.Code, pgErr.Message)
	message := defaultMessage
	if pgErr.Message != "" {
		message = defaultMessage + ": " + pgErr.Message
	}
	return createError(code, message, http.StatusInternalServerError, err, customMessages)
}

func createError(code ErrorCode, defaultMessage string, httpCode int, err error, customMessages map[string]string) error {
	message := defaultMessage
	if customMsg, exists := customMessages[string(code)]; exists {
		message = customMsg
	}

	return &Error{
		Code:     code,
		Message:  message,
		HTTPCode: httpCode,
		Err:      err,
	}
}
