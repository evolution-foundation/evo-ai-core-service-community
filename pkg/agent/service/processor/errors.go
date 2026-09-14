package processor

import (
	"errors"
	"fmt"
	"net/http"

	apiErrors "evo-ai-core-service/internal/httpclient/errors"

	"gorm.io/gorm"
)

func invalidf(format string, args ...interface{}) error {
	return apiErrors.New(apiErrors.ValidationError, fmt.Sprintf(format, args...), http.StatusBadRequest)
}

// withPrefix names the config key a nested rejection came from. The handler only
// recognizes an unwrapped *ApiError, so the classification is copied, not wrapped.
// Anything else is a failure of ours and travels untouched: prefixing it would
// open a 500 by calling the caller's input invalid.
func withPrefix(prefix string, err error) error {
	var apiErr *apiErrors.ApiError
	if errors.As(err, &apiErr) {
		return apiErrors.New(apiErr.Code, prefix+apiErr.Message, apiErr.HTTPCode)
	}
	return err
}

func isNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
