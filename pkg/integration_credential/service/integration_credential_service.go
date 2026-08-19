package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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

// ReferenceLookup names who holds ONE credential. Narrower than the index the
// listing builds: the guard asks about a single row and must hear a read
// failure, never an empty list.
type ReferenceLookup interface {
	ConsumersOf(ctx context.Context, id uuid.UUID) ([]string, error)
}

// DeleteConflictDetails is the 409 payload. The labels are display strings for
// the screen, which is why they travel here and not inside the message.
type DeleteConflictDetails struct {
	Consumers []string `json:"consumers"`
}

type integrationCredentialService struct {
	repository  repository.IntegrationCredentialRepository
	references  ReferenceLookup
	connections ConnectionReader
}

func NewIntegrationCredentialService(repo repository.IntegrationCredentialRepository, references ReferenceLookup, connections ConnectionReader) IntegrationCredentialService {
	return &integrationCredentialService{repository: repo, references: references, connections: connections}
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
	credential, err := s.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if err := s.refuseIfConnected(ctx, credential); err != nil {
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
// // Residual race: a consumer that starts referencing the credential between
// this check and the delete still ends up dangling. Closing it needs the delete
// to also rewrite the consumers' jsonb, which CRM-191 deferred on purpose; a
// transaction here would not close it either (READ COMMITTED would not see the
// new row) and would fight the tenant reroute, which swaps the connection
// before `gorm:begin_transaction`.
func (s *integrationCredentialService) refuseIfReferenced(ctx context.Context, id uuid.UUID) error {
	if s.references == nil {
		return apiErrors.New(
			apiErrors.InternalError,
			"integration credential delete is not wired to its consumer guard",
			http.StatusInternalServerError,
		)
	}

	consumers, err := s.references.ConsumersOf(ctx, id)
	if err != nil {
		return err
	}

	if len(consumers) == 0 {
		return nil
	}

	return conflict(consumers)
}

// refuseIfConnected covers the oauth kind, whose owner is NOT reachable through
// the jsonb refs: the vault row points at the connection by (owner_store,
// owner_ref), so a live connection never shows up as a consumer. Without this
// the row is deleted and the listing sync recreates it on the next page load
// under a NEW id, orphaning every ref that named the old one.
func (s *integrationCredentialService) refuseIfConnected(ctx context.Context, credential *model.IntegrationCredential) error {
	if credential == nil || credential.Kind != model.KindOAuth || credential.OwnerRef == nil {
		return nil
	}
	if credential.OwnerStore == nil || *credential.OwnerStore != model.OwnerStoreAgentIntegration {
		return nil
	}
	if s.connections == nil {
		return apiErrors.New(
			apiErrors.InternalError,
			"integration credential delete is not wired to its connection guard",
			http.StatusInternalServerError,
		)
	}

	connections, err := s.connections.LiveConnections(ctx)
	if err != nil {
		return err
	}

	// Mirrors the condition under which the sync would recreate the row, so
	// what the delete removes stays removed.
	for _, connection := range connections {
		if connection.IntegrationID != *credential.OwnerRef || !model.IsConnectionProvider(connection.Provider) {
			continue
		}

		return conflict([]string{fmt.Sprintf("Integração %s", connection.Provider)})
	}

	return nil
}

// conflict carries the consumers in details, not in the message: they are
// pt-BR display strings, and joining them into an English sentence gave a
// client no way to split a list whose items may contain a comma.
func conflict(consumers []string) error {
	return apiErrors.New(
		apiErrors.Conflict,
		fmt.Sprintf("integration credential is still in use by %d consumer(s)", len(consumers)),
		http.StatusConflict,
	).WithDetails(DeleteConflictDetails{Consumers: consumers})
}
