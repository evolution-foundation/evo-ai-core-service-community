package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	apiErrors "evo-ai-core-service/internal/httpclient/errors"
	"evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/integration_credential/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type stubCredentialRepo struct {
	stored    *model.IntegrationCredential
	getErr    error
	deleted   bool
	deleteErr error
	calls     int
}

func (r *stubCredentialRepo) Create(context.Context, model.IntegrationCredential) (*model.IntegrationCredential, error) {
	return nil, nil
}
func (r *stubCredentialRepo) GetByID(context.Context, uuid.UUID) (*model.IntegrationCredential, error) {
	return r.stored, r.getErr
}
func (r *stubCredentialRepo) List(context.Context, model.IntegrationCredentialListRequest) ([]*model.IntegrationCredential, error) {
	return nil, nil
}
func (r *stubCredentialRepo) Count(context.Context, model.IntegrationCredentialListRequest) (int64, error) {
	return 0, nil
}
func (r *stubCredentialRepo) Update(context.Context, *model.IntegrationCredential, *bool, uuid.UUID) (*model.IntegrationCredential, error) {
	return nil, nil
}
func (r *stubCredentialRepo) Delete(context.Context, uuid.UUID) (bool, error) {
	r.calls++
	return r.deleted, r.deleteErr
}

type referencedStub struct {
	id     uuid.UUID
	labels []string
	err    error
	calls  int
}

func (r *referencedStub) Build(context.Context) (ReferenceIndex, error) {
	r.calls++
	if r.err != nil {
		return ReferenceIndex{}, r.err
	}
	if len(r.labels) == 0 {
		return ReferenceIndex{}, nil
	}
	return ReferenceIndex{r.id: r.labels}, nil
}

func notFoundStatus(t *testing.T, err error) {
	t.Helper()
	var dbErr *postgres.Error
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected a mapped db error, got %T: %v", err, err)
	}
	if dbErr.HTTPCode != http.StatusNotFound || dbErr.Code != postgres.ERR_RECORD_NOT_FOUND {
		t.Fatalf("expected 404 %s, got %d %s", postgres.ERR_RECORD_NOT_FOUND, dbErr.HTTPCode, dbErr.Code)
	}
}

func TestDeleteSucceedsWhenTheRowIsRemoved(t *testing.T) {
	repo := &stubCredentialRepo{stored: &model.IntegrationCredential{}, deleted: true}
	refs := &referencedStub{}
	svc := NewIntegrationCredentialService(repo, refs)

	if err := svc.Delete(context.Background(), uuid.New()); err != nil {
		t.Fatalf("expected a successful delete, got %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("expected the repository delete once, got %d", repo.calls)
	}
}

func TestDeleteOfUnknownCredentialIsNotFound(t *testing.T) {
	repo := &stubCredentialRepo{getErr: postgres.MapDBError(gorm.ErrRecordNotFound, model.IntegrationCredentialErrors)}
	refs := &referencedStub{}
	svc := NewIntegrationCredentialService(repo, refs)

	notFoundStatus(t, svc.Delete(context.Background(), uuid.New()))
	if repo.calls != 0 {
		t.Fatalf("must not attempt the delete of an unknown credential, got %d calls", repo.calls)
	}
	if refs.calls != 0 {
		t.Fatalf("must not check consumers before confirming the credential exists, got %d calls", refs.calls)
	}
}

func TestDeleteMapsARepositoryFailure(t *testing.T) {
	repo := &stubCredentialRepo{stored: &model.IntegrationCredential{}, deleteErr: gorm.ErrInvalidData}
	refs := &referencedStub{}
	svc := NewIntegrationCredentialService(repo, refs)

	err := svc.Delete(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected a mapped failure, got nil")
	}
	var dbErr *postgres.Error
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected the repository error mapped, got %T: %v", err, err)
	}
}

func TestDeleteRacingWithAnotherDeleteIsNotFound(t *testing.T) {
	repo := &stubCredentialRepo{stored: &model.IntegrationCredential{}, deleted: false}
	refs := &referencedStub{}
	svc := NewIntegrationCredentialService(repo, refs)

	notFoundStatus(t, svc.Delete(context.Background(), uuid.New()))
}

func TestDeleteRefusesWhileACredentialHasConsumers(t *testing.T) {
	id := uuid.New()
	repo := &stubCredentialRepo{stored: &model.IntegrationCredential{}, deleted: true}
	refs := &referencedStub{id: id, labels: []string{"Agente Cobrança [api_key]", "MCP Zendesk [token]"}}
	svc := NewIntegrationCredentialService(repo, refs)

	err := svc.Delete(context.Background(), id)
	if err == nil {
		t.Fatal("expected the delete to be refused, got nil")
	}

	var apiErr *apiErrors.ApiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected an ApiError, got %T: %v", err, err)
	}
	if apiErr.HTTPCode != http.StatusConflict {
		t.Fatalf("expected 409, got %d", apiErr.HTTPCode)
	}
	for _, label := range refs.labels {
		if !strings.Contains(apiErr.Message, label) {
			t.Errorf("message %q does not name consumer %q", apiErr.Message, label)
		}
	}
	if repo.calls != 0 {
		t.Fatalf("must not delete while a consumer holds the credential, got %d calls", repo.calls)
	}
}

func TestDeleteProceedsWhenNoConsumersHoldTheCredential(t *testing.T) {
	id := uuid.New()
	repo := &stubCredentialRepo{stored: &model.IntegrationCredential{}, deleted: true}
	// Another credential has consumers; the one being deleted does not.
	refs := &referencedStub{id: uuid.New(), labels: []string{"Agente Cobrança [api_key]"}}
	svc := NewIntegrationCredentialService(repo, refs)

	if err := svc.Delete(context.Background(), id); err != nil {
		t.Fatalf("expected a successful delete, got %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("expected the repository delete once, got %d", repo.calls)
	}
}
