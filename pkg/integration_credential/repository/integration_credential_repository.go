package repository

import (
	"context"
	"time"

	"evo-ai-core-service/pkg/integration_credential/model"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IntegrationCredentialRepository interface {
	Create(ctx context.Context, credential model.IntegrationCredential) (*model.IntegrationCredential, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.IntegrationCredential, error)
	List(ctx context.Context, request model.IntegrationCredentialListRequest) ([]*model.IntegrationCredential, error)
	Count(ctx context.Context, request model.IntegrationCredentialListRequest) (int64, error)
	Update(ctx context.Context, credential *model.IntegrationCredential, isActive *bool, id uuid.UUID) (*model.IntegrationCredential, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
}

type integrationCredentialRepository struct {
	db *gorm.DB
}

func NewIntegrationCredentialRepository(db *gorm.DB) IntegrationCredentialRepository {
	return &integrationCredentialRepository{db: db}
}

func (r *integrationCredentialRepository) Create(ctx context.Context, credential model.IntegrationCredential) (*model.IntegrationCredential, error) {
	if err := r.db.WithContext(ctx).Create(&credential).Error; err != nil {
		return nil, err
	}

	return &credential, nil
}

func (r *integrationCredentialRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.IntegrationCredential, error) {
	var credential model.IntegrationCredential

	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&credential).Error; err != nil {
		return nil, err
	}

	return &credential, nil
}

func (r *integrationCredentialRepository) List(ctx context.Context, request model.IntegrationCredentialListRequest) ([]*model.IntegrationCredential, error) {
	var credentials []*model.IntegrationCredential

	query := applyFilters(r.db.WithContext(ctx), request)

	if err := query.Offset((request.Page - 1) * request.PageSize).Limit(request.PageSize).Find(&credentials).Error; err != nil {
		return []*model.IntegrationCredential{}, err
	}

	return credentials, nil
}

func (r *integrationCredentialRepository) Count(ctx context.Context, request model.IntegrationCredentialListRequest) (int64, error) {
	var count int64

	query := applyFilters(r.db.WithContext(ctx).Model(&model.IntegrationCredential{}), request)

	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

// applyFilters keeps List and Count on the same predicates: a page whose total
// came from a different filter set paginates wrong.
func applyFilters(query *gorm.DB, request model.IntegrationCredentialListRequest) *gorm.DB {
	// An empty filter returns BOTH states. The screen renders the
	// active/inactive badge and the reactivate action, so hiding inactive rows
	// by default (the api_key behavior this first mirrored) would make a
	// deactivated credential unreachable forever (adversarial review,
	// 2026-07-29). Consumers that only want active rows ask for active=true;
	// the CRM resolver filters is_active on its own query.
	if request.Active != "" {
		query = query.Where("is_active = ?", request.Active)
	}

	// An empty scope lists every scope, so the settings screen renders both
	// sections in one call.
	if request.Scope != "" {
		query = query.Where("scope = ?", request.Scope)
	}

	if request.Kind != "" {
		query = query.Where("kind = ?", request.Kind)
	}

	if request.Provider != "" {
		query = query.Where("provider = ?", request.Provider)
	}

	return query
}

func (r *integrationCredentialRepository) Update(ctx context.Context, credential *model.IntegrationCredential, isActive *bool, id uuid.UUID) (*model.IntegrationCredential, error) {
	credential.UpdatedAt = time.Now()
	if err := r.db.WithContext(ctx).Where("id = ?", id).Updates(credential).Error; err != nil {
		return nil, err
	}

	// GORM's struct Updates skips zero values, so `false` could never travel
	// through the struct: an explicit column update is what makes deactivation
	// possible at all .
	if isActive != nil {
		if err := r.db.WithContext(ctx).
			Model(&model.IntegrationCredential{}).
			Where("id = ?", id).
			Update("is_active", *isActive).Error; err != nil {
			return nil, err
		}
	}

	var updated model.IntegrationCredential
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&updated).Error; err != nil {
		return nil, err
	}

	return &updated, nil
}

func (r *integrationCredentialRepository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	if err := r.db.WithContext(ctx).Model(&model.IntegrationCredential{}).Where("id = ?", id).Update("is_active", false).Error; err != nil {
		return false, err
	}

	return true, nil
}
