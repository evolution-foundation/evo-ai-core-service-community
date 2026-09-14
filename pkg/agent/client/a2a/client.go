package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"evo-ai-core-service/internal/httpclient"
	apiErrors "evo-ai-core-service/internal/httpclient/errors"
	"fmt"
	"net/http"
)

type Client interface {
	FetchAgentCard(ctx context.Context, cardURL string) (map[string]interface{}, error)
}

type client struct {
}

func NewClient() Client {
	return &client{}
}

// A card_url that does not serve a card is the caller's to fix, so it answers 422.
// The response body is left out: echoing whatever an arbitrary URL returns would
// let the API read pages from networks only this service can reach.
func (c *client) FetchAgentCard(ctx context.Context, cardURL string) (map[string]interface{}, error) {
	response, err := httpclient.DoGetJSON[map[string]interface{}](ctx, cardURL, nil, http.StatusOK)
	if err != nil {
		reason := err.Error()

		var statusErr *httpclient.StatusError
		var syntaxErr *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &statusErr):
			reason = fmt.Sprintf("%s responded with HTTP %d", cardURL, statusErr.Code)
		case errors.As(err, &syntaxErr), errors.As(err, &typeErr):
			reason = notACard(cardURL)
		}

		return nil, cardFetchError(reason)
	}

	// DoGetJSON decodes an empty body or `null` into a nil map without an error.
	if response == nil || len(*response) == 0 {
		return nil, cardFetchError(notACard(cardURL))
	}

	return *response, nil
}

func notACard(cardURL string) string {
	return fmt.Sprintf("%s did not return a JSON agent card", cardURL)
}

func cardFetchError(reason string) error {
	return apiErrors.New(
		apiErrors.BusinessRuleViolation,
		"failed to fetch agent card: "+reason,
		http.StatusUnprocessableEntity,
	)
}
