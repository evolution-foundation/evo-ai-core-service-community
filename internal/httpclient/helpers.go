package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("HTTP request failed with status %d: %s", e.Code, e.Body)
}

func (e *StatusError) StatusCode() int {
	return e.Code
}

func SetHeaders(req *http.Request, headers map[string]string) {
	for k, v := range headers {
		req.Header.Set(k, v)
	}
}

func httpxClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     30 * time.Second,
			DisableKeepAlives:   false,
		},
	}
}

func DoPostJSON[Res any](
	ctx context.Context,
	url string,
	payload map[string]interface{},
	headers map[string]string,
	httpStatusCode int,
) (*Res, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	SetHeaders(req, headers)

	resp, err := httpxClient().Do(req)

	return DoJSON[Res](resp, err, httpStatusCode)
}

func DoGetJSON[Res any](
	ctx context.Context,
	url string,
	headers map[string]string,
	httpStatusCode int,
) (*Res, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	SetHeaders(req, headers)

	resp, err := httpxClient().Do(req)

	return DoJSON[Res](resp, err, httpStatusCode)
}

func DoPutJSON[Res any](
	ctx context.Context,
	url string,
	payload map[string]interface{},
	headers map[string]string,
	httpStatusCode int,
) (*Res, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	SetHeaders(req, headers)

	resp, err := httpxClient().Do(req)

	return DoJSON[Res](resp, err, httpStatusCode)
}

func DoDeleteJSON[Res any](
	ctx context.Context,
	url string,
	payload map[string]interface{},
	headers map[string]string,
	httpStatusCode int,
) (*Res, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	SetHeaders(req, headers)

	resp, err := httpxClient().Do(req)

	return DoJSON[Res](resp, err, httpStatusCode)
}

func DoJSON[Res any](
	resp *http.Response,
	err error,
	httpStatusCode int,
) (*Res, error) {
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != httpStatusCode {
		return nil, &StatusError{Code: resp.StatusCode, Body: string(body)}
	}

	var result Res

	if len(body) == 0 {
		return &result, nil
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &result, nil
}

func DoPostForm[Res any](
	ctx context.Context,
	destinationURL string,
	payload map[string]string,
	headers map[string]string,
	httpStatusCode int,
) (*Res, error) {
	formData := url.Values{}
	for k, v := range payload {
		formData.Set(k, fmt.Sprintf("%v", v))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, destinationURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	SetHeaders(req, headers)

	resp, err := httpxClient().Do(req)

	return DoJSON[Res](resp, err, httpStatusCode)
}
