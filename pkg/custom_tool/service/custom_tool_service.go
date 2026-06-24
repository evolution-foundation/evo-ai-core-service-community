package service

import (
	"context"
	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/internal/utils/stringutils"
	model "evo-ai-core-service/pkg/custom_tool/model"
	repository "evo-ai-core-service/pkg/custom_tool/repository"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// testHTTPClient is the dedicated client for the custom-tools test endpoint.
// It must NOT use the project's typed JSON helpers (DoGetJSON/DoPostJSON) because
// custom tools can target arbitrary endpoints returning HTML, plain text, XML, etc.
// We need to capture the raw response (status, headers, body) regardless of content-type.
var testHTTPClient = &http.Client{
	Timeout: 15 * time.Second,
}

// runToolTest issues the HTTP request described by the tool and returns a TestResult
// reflecting what actually happened: real status code, response time, headers, body.
// Success is determined by HTTP status (2xx) — not by whether the body parses as JSON.
func runToolTest(
	ctx context.Context,
	method, endpoint string,
	headers map[string]string,
) *model.TestResult {
	result := &model.TestResult{}

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), endpoint, nil)
	if err != nil {
		result.Error = fmt.Sprintf("failed to build request: %v", err)
		return result
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := testHTTPClient.Do(req)
	elapsed := time.Since(start)
	result.ResponseTime = elapsed.Seconds()

	if err != nil {
		msg := err.Error()
		switch {
		case strings.Contains(msg, "context deadline exceeded"), strings.Contains(msg, "Timeout"):
			result.Error = "Request timeout"
		case strings.Contains(msg, "no such host"):
			result.Error = "DNS resolution failed"
		case strings.Contains(msg, "connection refused"):
			result.Error = "Connection refused"
		default:
			result.Error = msg
		}
		return result
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		result.Error = fmt.Sprintf("failed to read response body: %v", readErr)
		result.StatusCode = resp.StatusCode
		return result
	}

	result.StatusCode = resp.StatusCode
	result.Body = string(body)
	result.Headers = make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			result.Headers[k] = v[0]
		}
	}
	result.Success = resp.StatusCode >= 200 && resp.StatusCode < 300
	if !result.Success {
		result.Error = fmt.Sprintf("HTTP %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	return result
}

type CustomToolService interface {
	Create(ctx context.Context, request model.CustomTool) (*model.CustomTool, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.CustomTool, error)
	List(ctx context.Context, request model.CustomToolListRequest) (*model.CustomToolListResponse, error)
	Update(ctx context.Context, request *model.CustomTool, id uuid.UUID) (*model.CustomTool, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	ConvertToHTTPTool(tool model.CustomToolResponse) map[string]interface{}
	Test(ctx context.Context, id uuid.UUID) (*model.CustomToolTestResponse, error)
}

type customToolService struct {
	customToolRepository repository.CustomToolRepository
}

func NewCustomToolService(customToolRepository repository.CustomToolRepository) CustomToolService {
	return &customToolService{
		customToolRepository: customToolRepository,
	}
}

func (s *customToolService) Create(ctx context.Context, request model.CustomTool) (*model.CustomTool, error) {
	customTool, err := s.customToolRepository.Create(ctx, request)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	return customTool, nil
}

func (s *customToolService) GetByID(ctx context.Context, id uuid.UUID) (*model.CustomTool, error) {
	customTool, err := s.customToolRepository.GetByID(ctx, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	return customTool, nil
}

func (s *customToolService) List(ctx context.Context, request model.CustomToolListRequest) (*model.CustomToolListResponse, error) {
	// Get paginated items
	customTools, err := s.customToolRepository.List(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	// Get total count
	totalItems, err := s.customToolRepository.Count(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	// Convert to response items
	items := make([]model.CustomToolResponse, len(customTools))
	for i, customTool := range customTools {
		items[i] = *customTool.ToResponse()
	}

	// Calculate pagination metadata
	totalPages := int((totalItems + int64(request.PageSize) - 1) / int64(request.PageSize))
	skip := (request.Page - 1) * request.PageSize
	limit := request.PageSize

	return &model.CustomToolListResponse{
		Items:      items,
		Page:       request.Page,
		PageSize:   request.PageSize,
		Skip:       skip,
		Limit:      limit,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}, nil
}

func (s *customToolService) Update(ctx context.Context, request *model.CustomTool, id uuid.UUID) (*model.CustomTool, error) {
	_, err := s.GetByID(ctx, id)

	if err != nil {
		return nil, err
	}

	customTool, err := s.customToolRepository.Update(ctx, request, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	return customTool, nil
}

func (s *customToolService) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	_, err := s.GetByID(ctx, id)

	if err != nil {
		return false, err
	}

	deleted, err := s.customToolRepository.Delete(ctx, id)

	if err != nil {
		return false, errorsPostgres.MapDBError(err, model.CustomToolErrors)
	}

	return deleted, nil
}

func (s *customToolService) ConvertToHTTPTool(tool model.CustomToolResponse) map[string]interface{} {
	var errorHandling map[string]interface{}
	if tool.ErrorHandling != nil {
		errorHandling = tool.ErrorHandling
	}

	if _, ok := errorHandling["timeout"]; !ok {
		errorHandling["timeout"] = 30
	}
	if _, ok := errorHandling["retry_count"]; !ok {
		errorHandling["retry_count"] = 0
	}
	if _, ok := errorHandling["fallback_response"]; !ok {
		errorHandling["fallback_response"] = map[string]string{
			"error":   "",
			"message": "",
		}
	}

	return map[string]interface{}{
		"name":     tool.Name,
		"method":   tool.Method,
		"endpoint": tool.Endpoint,
		"headers":  tool.Headers,
		"parameters": map[string]interface{}{
			"path_params":  tool.PathParams,
			"query_params": tool.QueryParams,
			"body_params":  tool.BodyParams,
		},
		"description":    tool.Description,
		"error_handling": errorHandling,
		"values":         tool.Values,
	}
}

func (s *customToolService) Test(ctx context.Context, id uuid.UUID) (*model.CustomToolTestResponse, error) {
	customTool, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	response := customTool.ToResponse()
	method := strings.ToUpper(customTool.Method)

	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodHead, http.MethodOptions:
		// supported
	default:
		return nil, fmt.Errorf("unsupported method: %s", customTool.Method)
	}

	headers := stringutils.JSONToStringMap(customTool.Headers)
	testResult := runToolTest(ctx, method, customTool.Endpoint, headers)

	return &model.CustomToolTestResponse{
		Tool:       response,
		TestResult: testResult,
	}, nil
}
