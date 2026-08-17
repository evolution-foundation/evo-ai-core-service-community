package service

import (
	"context"
	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/api_key/model"
	"evo-ai-core-service/pkg/api_key/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ApiKeyService interface {
	Create(ctx context.Context, request model.ApiKey) (*model.ApiKey, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.ApiKey, error)
	List(ctx context.Context, request model.ApiKeyListRequest) (*model.ApiKeyListResponse, error)
	Update(ctx context.Context, request *model.ApiKey, isActive *bool, id uuid.UUID) (*model.ApiKey, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type apiKeyService struct {
	apiKeyRepository repository.ApiKeyRepository
}

func NewApiKeyService(apiKeyRepository repository.ApiKeyRepository) ApiKeyService {
	return &apiKeyService{
		apiKeyRepository: apiKeyRepository,
	}
}

func (s *apiKeyService) Create(ctx context.Context, request model.ApiKey) (*model.ApiKey, error) {
	apiKey, err := s.apiKeyRepository.Create(ctx, request)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.APIKeyErrors)
	}

	return apiKey, nil
}

func (s *apiKeyService) GetByID(ctx context.Context, id uuid.UUID) (*model.ApiKey, error) {
	apiKey, err := s.apiKeyRepository.GetByID(ctx, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.APIKeyErrors)
	}

	return apiKey, nil
}

func (s *apiKeyService) List(ctx context.Context, request model.ApiKeyListRequest) (*model.ApiKeyListResponse, error) {
	// Get paginated items
	apiKeys, err := s.apiKeyRepository.List(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.APIKeyErrors)
	}

	// Get total count
	totalItems, err := s.apiKeyRepository.Count(ctx, request.Active, request.Scope)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.APIKeyErrors)
	}

	// Convert to response items
	items := make([]model.ApiKeyResponse, len(apiKeys))
	for i, apiKey := range apiKeys {
		items[i] = *apiKey.ToResponse()
	}

	// Calculate pagination metadata
	totalPages := int((totalItems + int64(request.PageSize) - 1) / int64(request.PageSize))
	skip := (request.Page - 1) * request.PageSize
	limit := request.PageSize

	return &model.ApiKeyListResponse{
		Items:      items,
		Page:       request.Page,
		PageSize:   request.PageSize,
		Skip:       skip,
		Limit:      limit,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}, nil
}

func (s *apiKeyService) Update(ctx context.Context, request *model.ApiKey, isActive *bool, id uuid.UUID) (*model.ApiKey, error) {
	if _, err := s.GetByID(ctx, id); err != nil {
		return nil, err
	}

	apiKey, err := s.apiKeyRepository.Update(ctx, request, isActive, id)

	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.APIKeyErrors)
	}

	return apiKey, nil
}

// Delete reports only failure: a nil error means the row is gone. Anything
// else — unknown id, or deleted between the read and the delete — is the
// mapped 404, never a silent success.
func (s *apiKeyService) Delete(ctx context.Context, id uuid.UUID) error {
	// GetByID already maps a missing row to the 404 error; wrapping it in a
	// plain error used to surface as 500.
	if _, err := s.GetByID(ctx, id); err != nil {
		return err
	}

	deleted, err := s.apiKeyRepository.Delete(ctx, id)
	if err != nil {
		return errorsPostgres.MapDBError(err, model.APIKeyErrors)
	}

	if !deleted {
		return errorsPostgres.MapDBError(gorm.ErrRecordNotFound, model.APIKeyErrors)
	}

	return nil
}
