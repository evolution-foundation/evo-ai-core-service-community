package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	apiErrors "evo-ai-core-service/internal/httpclient/errors"
	errorsPostgres "evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/integration_credential/model"
	"evo-ai-core-service/pkg/integration_credential/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IntegrationCredentialService interface {
	Create(ctx context.Context, request model.IntegrationCredential) (*model.IntegrationCredential, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.IntegrationCredential, error)
	List(ctx context.Context, request model.IntegrationCredentialListRequest) (*model.IntegrationCredentialListResponse, error)
	Update(ctx context.Context, request *model.IntegrationCredential, isActive *bool, id uuid.UUID) (*model.IntegrationCredential, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type ReferenceIndexer interface {
	Build(ctx context.Context) (ReferenceIndex, error)
}

type integrationCredentialService struct {
	repository repository.IntegrationCredentialRepository
	references ReferenceIndexer
}

func NewIntegrationCredentialService(repo repository.IntegrationCredentialRepository, references ReferenceIndexer) IntegrationCredentialService {
	return &integrationCredentialService{repository: repo, references: references}
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

func (s *integrationCredentialService) Update(ctx context.Context, request *model.IntegrationCredential, isActive *bool, id uuid.UUID) (*model.IntegrationCredential, error) {
	if _, err := s.GetByID(ctx, id); err != nil {
		return nil, errors.New("Integration credential not found")
	}

	credential, err := s.repository.Update(ctx, request, isActive, id)
	if err != nil {
		return nil, errorsPostgres.MapDBError(err, model.IntegrationCredentialErrors)
	}

	return credential, nil
}

func (s *integrationCredentialService) Delete(ctx context.Context, id uuid.UUID) error {
	// GetByID already maps a missing row to the 404 error; wrapping it in a
	// plain error used to surface as 500.
	if _, err := s.GetByID(ctx, id); err != nil {
		return err
	}

	if err := s.refuseIfReferenced(ctx, id); err != nil {
		return err
	}

	deleted, err := s.repository.Delete(ctx, id)
	if err != nil {
		return errorsPostgres.MapDBError(err, model.IntegrationCredentialErrors)
	}

	if !deleted {
		return errorsPostgres.MapDBError(gorm.ErrRecordNotFound, model.IntegrationCredentialErrors)
	}

	return nil
}

// No FK backs this table, unlike api_key: a hard delete would otherwise leave
// a dangling jsonb reference instead of nulling it.
func (s *integrationCredentialService) refuseIfReferenced(ctx context.Context, id uuid.UUID) error {
	index, err := s.references.Build(ctx)
	if err != nil {
		return err
	}

	consumers := index.For(id)
	if len(consumers) == 0 {
		return nil
	}

	return apiErrors.New(
		apiErrors.Conflict,
		fmt.Sprintf("integration credential is still in use by: %s", strings.Join(consumers, ", ")),
		http.StatusConflict,
	)
}
