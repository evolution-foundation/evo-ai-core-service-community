package service

import (
	"context"
	"errors"
	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/mcp_server/model"
	"evo-ai-core-service/pkg/mcp_server/repository"

	"github.com/google/uuid"
)

type McpServerService interface {
	Create(ctx context.Context, request model.McpServer) (*model.McpServer, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.McpServer, error)
	List(ctx context.Context, page int, pageSize int, search string) (*model.McpServerListResponse, error)
	Update(ctx context.Context, request *model.McpServer, id uuid.UUID) (*model.McpServer, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
}

type mcpServerService struct {
	mcpServerRepository repository.McpServerRepository
}

func NewMcpServerService(mcpServerRepository repository.McpServerRepository) McpServerService {
	return &mcpServerService{
		mcpServerRepository: mcpServerRepository,
	}
}

func (s *mcpServerService) Create(ctx context.Context, request model.McpServer) (*model.McpServer, error) {
	mcpServer, err := s.mcpServerRepository.Create(ctx, request)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.MCPServerErrors)
	}

	return mcpServer, nil
}

func (s *mcpServerService) GetByID(ctx context.Context, id uuid.UUID) (*model.McpServer, error) {
	mcpServer, err := s.mcpServerRepository.GetByID(ctx, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.MCPServerErrors)
	}

	return mcpServer, nil
}

func (s *mcpServerService) List(ctx context.Context, page int, pageSize int, search string) (*model.McpServerListResponse, error) {
	// Get paginated items
	mcpServers, err := s.mcpServerRepository.List(ctx, page, pageSize, search)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.MCPServerErrors)
	}

	// Get total count
	totalItems, err := s.mcpServerRepository.Count(ctx, search)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.MCPServerErrors)
	}

	// Convert to response items
	items := make([]model.McpServerResponse, len(mcpServers))
	for i, mcpServer := range mcpServers {
		items[i] = *mcpServer.ToResponse()
	}

	// Calculate pagination metadata
	totalPages := int((totalItems + int64(pageSize) - 1) / int64(pageSize))
	skip := (page - 1) * pageSize
	limit := pageSize

	return &model.McpServerListResponse{
		Items:      items,
		Page:       page,
		PageSize:   pageSize,
		Skip:       skip,
		Limit:      limit,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}, nil
}

func (s *mcpServerService) Update(ctx context.Context, request *model.McpServer, id uuid.UUID) (*model.McpServer, error) {
	_, err := s.GetByID(ctx, id)

	if err != nil {
		return nil, errors.New("MCP server not found")
	}

	mcpServer, err := s.mcpServerRepository.Update(ctx, request, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.MCPServerErrors)
	}

	return mcpServer, nil
}

func (s *mcpServerService) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	_, err := s.GetByID(ctx, id)

	if err != nil {
		return false, errors.New("MCP server not found")
	}

	deleted, err := s.mcpServerRepository.Delete(ctx, id)

	if err != nil {
		return false, errorsPostgres.MapDBError(err, model.MCPServerErrors)
	}

	return deleted, nil
}
