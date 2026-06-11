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
	"strings"
	"time"

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

	url := customMcpServer.URL
	if !strings.HasSuffix(url, "/health") {
		url = strings.TrimRight(url, "/") + "/health"
	}

	headers := stringutils.JSONToStringMap(customMcpServer.Headers)
	start := time.Now()

	type HealthResponse struct{}
	_, err = httpclient.DoGetJSON[HealthResponse](ctx, url, headers, http.StatusOK)
	elapsed := time.Since(start)

	response := customMcpServer.ToResponse()
	testResult := &model.TestResult{
		URLTested: url,
	}

	testResult.Success = true
	testResult.StatusCode = http.StatusOK
	testResult.ResponseTime = elapsed.Seconds()
	testResult.Message = "Connection successful"

	if err != nil {
		testResult.StatusCode = http.StatusInternalServerError
		testResult.Success = false
		testResult.Error = err.Error()
	}

	return &model.CustomMcpServerTestResponse{
		Server:     response,
		TestResult: testResult,
	}, nil
}
