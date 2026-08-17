package service

import (
	"context"
	"testing"

	"evo-ai-core-service/internal/infra/postgres"
	"evo-ai-core-service/pkg/api_key/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Same defect the delete carried: the lookup error was replaced by a plain
// errors.New, which HandleError only knows how to turn into a 500. The mapped
// 404 from the lookup is what must reach the client.
func TestUpdateOfUnknownKeyIsNotFound(t *testing.T) {
	repo := &stubRepo{getErr: postgres.MapDBError(gorm.ErrRecordNotFound, model.APIKeyErrors)}
	svc := NewApiKeyService(repo)

	updated, err := svc.Update(context.Background(), &model.ApiKey{}, nil, uuid.New())
	if updated != nil {
		t.Fatal("must not report an update that never happened")
	}
	notFoundStatus(t, err)
}
