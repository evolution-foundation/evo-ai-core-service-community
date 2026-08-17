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

type deleteStubRepo struct {
	stored    *model.ApiKey
	getErr    error
	deleted   bool
	deleteErr error
	calls     int
}

func (r *deleteStubRepo) Create(context.Context, model.ApiKey) (*model.ApiKey, error) {
	return nil, nil
}
func (r *deleteStubRepo) GetByID(context.Context, uuid.UUID) (*model.ApiKey, error) {
	return r.stored, r.getErr
}
func (r *deleteStubRepo) List(context.Context, model.ApiKeyListRequest) ([]*model.ApiKey, error) {
	return nil, nil
}
func (r *deleteStubRepo) Count(context.Context, string, string) (int64, error) { return 0, nil }
func (r *deleteStubRepo) Update(context.Context, *model.ApiKey, *bool, uuid.UUID) (*model.ApiKey, error) {
	return nil, nil
}
func (r *deleteStubRepo) Delete(context.Context, uuid.UUID) (bool, error) {
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

func TestDeleteReturnsTrueWhenTheRowIsRemoved(t *testing.T) {
	repo := &deleteStubRepo{stored: &model.ApiKey{}, deleted: true}
	svc := NewApiKeyService(repo)

	deleted, err := svc.Delete(context.Background(), uuid.New())
	if err != nil || !deleted {
		t.Fatalf("expected a successful delete, got %v %v", deleted, err)
	}
	if repo.calls != 1 {
		t.Fatalf("expected the repository delete once, got %d", repo.calls)
	}
}

// A missing key used to come back as a plain error, which the handler turned
// into a 500; the mapped 404 from the lookup is what must reach the client.
func TestDeleteOfUnknownKeyIsNotFound(t *testing.T) {
	repo := &deleteStubRepo{getErr: postgres.MapDBError(gorm.ErrRecordNotFound, model.APIKeyErrors)}
	svc := NewApiKeyService(repo)

	deleted, err := svc.Delete(context.Background(), uuid.New())
	if deleted {
		t.Fatal("must not report a delete that never happened")
	}
	notFoundStatus(t, err)
	if repo.calls != 0 {
		t.Fatalf("must not attempt the delete of an unknown key, got %d calls", repo.calls)
	}
}

// Deleted by someone else between the read and the delete: still a 404, never
// a silent success.
func TestDeleteRacingWithAnotherDeleteIsNotFound(t *testing.T) {
	repo := &deleteStubRepo{stored: &model.ApiKey{}, deleted: false}
	svc := NewApiKeyService(repo)

	deleted, err := svc.Delete(context.Background(), uuid.New())
	if deleted {
		t.Fatal("zero rows affected must not read as deleted")
	}
	notFoundStatus(t, err)
}
