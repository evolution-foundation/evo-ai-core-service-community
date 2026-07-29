package service

import (
	"context"
	"errors"

	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/integration_credential/model"
	"evo-ai-core-service/pkg/integration_credential/repository"

	"github.com/google/uuid"
)

type IntegrationCredentialService interface {
	Create(ctx context.Context, request model.IntegrationCredential) (*model.IntegrationCredential, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.IntegrationCredential, error)
	List(ctx context.Context, request model.IntegrationCredentialListRequest) (*model.IntegrationCredentialListResponse, error)
	Update(ctx context.Context, request *model.IntegrationCredential, id uuid.UUID) (*model.IntegrationCredential, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
}

type integrationCredentialService struct {
	repository repository.IntegrationCredentialRepository
}

func NewIntegrationCredentialService(repo repository.IntegrationCredentialRepository) IntegrationCredentialService {
	return &integrationCredentialService{repository: repo}
}

func (s *integrationCredentialService) Create(ctx context.Context, request model.IntegrationCredential) (*model.IntegrationCredential, error) {
	credential, err := s.repository.Create(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.IntegrationCredentialErrors)
	}

	return credential, nil
}

func (s *integrationCredentialService) GetByID(ctx context.Context, id uuid.UUID) (*model.IntegrationCredential, error) {
	credential, err := s.repository.GetByID(ctx, id)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.IntegrationCredentialErrors)
	}

	return credential, nil
}

func (s *integrationCredentialService) List(ctx context.Context, request model.IntegrationCredentialListRequest) (*model.IntegrationCredentialListResponse, error) {
	credentials, err := s.repository.List(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.IntegrationCredentialErrors)
	}

	totalItems, err := s.repository.Count(ctx, request)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.IntegrationCredentialErrors)
	}

	items := make([]model.IntegrationCredentialResponse, len(credentials))
	for i, credential := range credentials {
		items[i] = *credential.ToResponse()
	}

	totalPages := int((totalItems + int64(request.PageSize) - 1) / int64(request.PageSize))
	skip := (request.Page - 1) * request.PageSize

	return &model.IntegrationCredentialListResponse{
		Items:      items,
		Page:       request.Page,
		PageSize:   request.PageSize,
		Skip:       skip,
		Limit:      request.PageSize,
		TotalItems: totalItems,
		TotalPages: totalPages,
	}, nil
}

func (s *integrationCredentialService) Update(ctx context.Context, request *model.IntegrationCredential, id uuid.UUID) (*model.IntegrationCredential, error) {
	if _, err := s.GetByID(ctx, id); err != nil {
		return nil, errors.New("Integration credential not found")
	}

	credential, err := s.repository.Update(ctx, request, id)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.IntegrationCredentialErrors)
	}

	return credential, nil
}

func (s *integrationCredentialService) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	if _, err := s.GetByID(ctx, id); err != nil {
		return false, errors.New("Integration credential not found")
	}

	deleted, err := s.repository.Delete(ctx, id)
	if err != nil {
		return false, errorsPostgres.MapDBError(err, model.IntegrationCredentialErrors)
	}

	return deleted, nil
}
