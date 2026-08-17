package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/api_key/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type stubRepo struct {
	stored    *model.ApiKey
	getErr    error
	deleted   bool
	deleteErr error
	calls     int
}

func (r *stubRepo) Create(context.Context, model.ApiKey) (*model.ApiKey, error) {
	return nil, nil
}
func (r *stubRepo) GetByID(context.Context, uuid.UUID) (*model.ApiKey, error) {
	return r.stored, r.getErr
}
func (r *stubRepo) List(context.Context, model.ApiKeyListRequest) ([]*model.ApiKey, error) {
	return nil, nil
}
func (r *stubRepo) Count(context.Context, string, string) (int64, error) { return 0, nil }
func (r *stubRepo) Update(context.Context, *model.ApiKey, *bool, uuid.UUID) (*model.ApiKey, error) {
	return nil, nil
}
func (r *stubRepo) Delete(context.Context, uuid.UUID) (bool, error) {
	r.calls++
	return r.deleted, r.deleteErr
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
	repo := &stubRepo{stored: &model.ApiKey{}, deleted: true}
	svc := NewApiKeyService(repo)

	if err := svc.Delete(context.Background(), uuid.New()); err != nil {
		t.Fatalf("expected a successful delete, got %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("expected the repository delete once, got %d", repo.calls)
	}
}

// A missing key used to come back as a plain error, which the handler turned
// into a 500; the mapped 404 from the lookup is what must reach the client.
func TestDeleteOfUnknownKeyIsNotFound(t *testing.T) {
	repo := &stubRepo{getErr: postgres.MapDBError(gorm.ErrRecordNotFound, model.APIKeyErrors)}
	svc := NewApiKeyService(repo)

	notFoundStatus(t, svc.Delete(context.Background(), uuid.New()))
	if repo.calls != 0 {
		t.Fatalf("must not attempt the delete of an unknown key, got %d calls", repo.calls)
	}
}

// A database failure on the delete itself must reach the client as the mapped
// error, not as a bare success or a bare 500.
func TestDeleteMapsARepositoryFailure(t *testing.T) {
	repo := &stubRepo{stored: &model.ApiKey{}, deleteErr: gorm.ErrInvalidData}
	svc := NewApiKeyService(repo)

	err := svc.Delete(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected a mapped failure, got nil")
	}
	var dbErr *postgres.Error
	if !errors.As(err, &dbErr) {
		t.Fatalf("expected the repository error mapped, got %T: %v", err, err)
	}
}

// Deleted by someone else between the read and the delete: still a 404, never
// a silent success.
func TestDeleteRacingWithAnotherDeleteIsNotFound(t *testing.T) {
	repo := &stubRepo{stored: &model.ApiKey{}, deleted: false}
	svc := NewApiKeyService(repo)

	notFoundStatus(t, svc.Delete(context.Background(), uuid.New()))
}
