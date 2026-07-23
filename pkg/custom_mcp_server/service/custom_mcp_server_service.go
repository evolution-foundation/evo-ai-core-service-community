package service

import (
	"context"
	"errors"
	"evo-ai-core-service/internal/config"
	"evo-ai-core-service/internal/httpclient"
	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/internal/utils/contextutils"
	"evo-ai-core-service/internal/utils/stringutils"

	model "evo-ai-core-service/pkg/custom_mcp_server/model"
	repository "evo-ai-core-service/pkg/custom_mcp_server/repository"
	"evo-ai-core-service/pkg/evoextensions/runtimecontext"
	"fmt"
	"net/http"

	"github.com/google/uuid"
)

type CustomMcpServerService interface {
	Create(ctx context.Context, request model.CustomMcpServer) (*model.CustomMcpServer, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.CustomMcpServer, error)
	List(ctx context.Context, request model.CustomMcpServerListRequest) (*model.CustomMcpServerListResponse, error)
	Update(ctx context.Context, request *model.CustomMcpServer, id uuid.UUID) (*model.CustomMcpServer, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	GetByAgentConfig(ctx context.Context, serverIDs []uuid.UUID) ([]*model.CustomMcpServer, error)
	Test(ctx context.Context, id uuid.UUID) (*model.CustomMcpServerTestResponse, error)
	// EVO-1739: stateless test of an UNSAVED server's url/headers, so the wizard can
	// "test before save". Reuses the same processor MCP handshake as Test.
	TestConnection(ctx context.Context, url string, headers map[string]string) (*model.TestResult, error)
}

type customMcpServerService struct {
	customMcpServerRepository repository.CustomMcpServerRepository
	cfgAIProcessorService     *config.AIProcessorServiceConfig
}

func NewCustomMcpServerService(customMcpServerRepository repository.CustomMcpServerRepository, cfgAIProcessorService *config.AIProcessorServiceConfig) CustomMcpServerService {
	return &customMcpServerService{
		customMcpServerRepository: customMcpServerRepository,
		cfgAIProcessorService:     cfgAIProcessorService,
	}
}

func (s *customMcpServerService) Create(ctx context.Context, request model.CustomMcpServer) (*model.CustomMcpServer, error) {
	tools, err := s.discoverTools(ctx, request)

	if err != nil {
		return nil, errors.New("Failed to discover tools")
	}

	request.Tools = stringutils.InterfaceMapSliceToJSON(tools.Tools)

	customMcpServer, err := s.customMcpServerRepository.Create(ctx, request)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	return customMcpServer, nil
}

func (s *customMcpServerService) GetByID(ctx context.Context, id uuid.UUID) (*model.CustomMcpServer, error) {
	customMcpServer, err := s.customMcpServerRepository.GetByID(ctx, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	return customMcpServer, nil
}

func (s *customMcpServerService) List(ctx context.Context, request model.CustomMcpServerListRequest) (*model.CustomMcpServerListResponse, error) {
	// Get paginated items
	customMcpServers, err := s.customMcpServerRepository.List(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	// Get total count
	totalItems, err := s.customMcpServerRepository.Count(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	// Convert to response items
	items := make([]model.CustomMcpServerResponse, len(customMcpServers))
	for i, customMcpServer := range customMcpServers {
		items[i] = *customMcpServer.ToResponse()
	}

	// Calculate pagination metadata
	totalPages := int((totalItems + int64(request.PageSize) - 1) / int64(request.PageSize))
	skip := (request.Page - 1) * request.PageSize
	limit := request.PageSize

	return &model.CustomMcpServerListResponse{
		Items:      items,
		Page:       request.Page,
		PageSize:   request.PageSize,
		Skip:       skip,
		Limit:      limit,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}, nil
}

func (s *customMcpServerService) Update(ctx context.Context, request *model.CustomMcpServer, id uuid.UUID) (*model.CustomMcpServer, error) {
	_, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	tools, err := s.discoverTools(ctx, *request)

	if err != nil {
		return nil, errors.New("Failed to discover tools")
	}

	request.Tools = stringutils.InterfaceMapSliceToJSON(tools.Tools)

	customMcpServer, err := s.customMcpServerRepository.Update(ctx, request, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	return customMcpServer, nil
}

func (s *customMcpServerService) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	_, err := s.GetByID(ctx, id)
	if err != nil {
		return false, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	deleted, err := s.customMcpServerRepository.Delete(ctx, id)

	if err != nil {
		return false, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	return deleted, nil
}

func (s *customMcpServerService) discoverTools(ctx context.Context, request model.CustomMcpServer) (*model.CustomMcpServerToolsResponse, error) {
	token, err := contextutils.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	headers := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": fmt.Sprintf("Bearer %s", token),
	}

	// EVO-1623 (GO-4): propagate the active tenant id to the processor
	// so its runtime_context middleware (PY-1) can authorize the call.
	// The community runtimecontext returns "" in standalone builds; we
	// only attach the header when an enterprise scope has bound a real
	// tenant id, satisfying the "no tenant → omit header" AC.
	//
	// The header name is the literal `X-Evo-Tenant-Id` — keep in sync
	// with `tenant.HeaderTenantID` in evo-enterprise-licensing-go. We
	// intentionally do NOT import the enterprise SDK constant here to
	// preserve the community/enterprise decoupling that `runtimecontext`
	// exists to enforce; the cross-repo contract is asserted by PY-1's
	// integration tests instead.
	if tenantID := runtimecontext.IDFromContext(ctx); tenantID != "" {
		headers["X-Evo-Tenant-Id"] = tenantID
	}

	tools, err := httpclient.DoPostJSON[model.CustomMcpServerToolsResponse](
		ctx,
		fmt.Sprintf("%s/api/%s/custom-mcp-servers/discover-tools", s.cfgAIProcessorService.URL, s.cfgAIProcessorService.Version),
		map[string]interface{}{
			"url":     request.URL,
			"headers": stringutils.JSONToStringMap(request.Headers),
		},
		headers,
		http.StatusOK,
	)

	if err != nil {
		return nil, err
	}

	return tools, nil
}

func (s *customMcpServerService) GetByAgentConfig(ctx context.Context, serverIDs []uuid.UUID) ([]*model.CustomMcpServer, error) {
	servers, err := s.customMcpServerRepository.GetByAgentConfig(ctx, serverIDs)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	return servers, nil
}

func (s *customMcpServerService) Test(ctx context.Context, id uuid.UUID) (*model.CustomMcpServerTestResponse, error) {
	customMcpServer, err := s.GetByID(ctx, id)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.CustomMCPServerErrors)
	}

	testResult, err := s.testConnection(ctx, customMcpServer.URL, stringutils.JSONToStringMap(customMcpServer.Headers))
	if err != nil {
		return nil, err
	}

	return &model.CustomMcpServerTestResponse{
		Server:     customMcpServer.ToResponse(),
		TestResult: testResult,
	}, nil
}

// TestConnection runs the MCP handshake against arbitrary url/headers without a saved
// server — powers the wizard's "test before save" (EVO-1739).
func (s *customMcpServerService) TestConnection(ctx context.Context, url string, headers map[string]string) (*model.TestResult, error) {
	return s.testConnection(ctx, url, headers)
}

// EVO-2139: delegate the MCP connection test to the processor, which owns
// the real handshake (POST JSON-RPC 2.0 `initialize`) via Google ADK's
// MCPToolset. The previous implementation did a raw `GET /health` from Go
// and failed for every compliant MCP server — the route does not exist in
// the MCP spec. Mirrors the discoverTools delegation pattern above,
// including X-Evo-Tenant-Id propagation (see EVO-1623).
func (s *customMcpServerService) testConnection(ctx context.Context, url string, headers map[string]string) (*model.TestResult, error) {
	token, err := contextutils.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	reqHeaders := map[string]string{
		"Content-Type":  "application/json",
		"Authorization": fmt.Sprintf("Bearer %s", token),
	}
	if tenantID := runtimecontext.IDFromContext(ctx); tenantID != "" {
		reqHeaders["X-Evo-Tenant-Id"] = tenantID
	}

	type processorTestResponse struct {
		Success      bool    `json:"success"`
		StatusCode   int     `json:"status_code"`
		ResponseTime float64 `json:"response_time"`
		URLTested    string  `json:"url_tested"`
		Message      string  `json:"message"`
		Error        string  `json:"error"`
		ToolsCount   int     `json:"tools_count"`
	}

	// Processor always returns 200 with the result envelope (success/failure
	// inside `success`), matching the discover-tools pattern.
	resp, err := httpclient.DoPostJSON[processorTestResponse](
		ctx,
		fmt.Sprintf("%s/api/%s/custom-mcp-servers/test-connection", s.cfgAIProcessorService.URL, s.cfgAIProcessorService.Version),
		map[string]interface{}{
			"url":     url,
			"headers": headers,
		},
		reqHeaders,
		http.StatusOK,
	)
	if err != nil {
		return &model.TestResult{
			Success:   false,
			URLTested: url,
			Error:     err.Error(),
		}, nil
	}

	statusCode := resp.StatusCode
	if statusCode == 0 {
		if resp.Success {
			statusCode = http.StatusOK
		} else {
			statusCode = http.StatusBadGateway
		}
	}

	return &model.TestResult{
		Success:      resp.Success,
		StatusCode:   statusCode,
		ResponseTime: resp.ResponseTime,
		URLTested:    resp.URLTested,
		Message:      resp.Message,
		Error:        resp.Error,
		ToolsCount:   resp.ToolsCount,
	}, nil
}
