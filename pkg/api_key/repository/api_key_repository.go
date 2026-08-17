package repository

import (
	"context"
	"evo-ai-core-service/pkg/api_key/model"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ApiKeyRepository interface {
	Create(ctx context.Context, apiKey model.ApiKey) (*model.ApiKey, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.ApiKey, error)
	List(ctx context.Context, request model.ApiKeyListRequest) ([]*model.ApiKey, error)
	Count(ctx context.Context, active string, scope string) (int64, error)
	Update(ctx context.Context, apiKey *model.ApiKey, isActive *bool, id uuid.UUID) (*model.ApiKey, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
}

type apiKeyRepository struct {
	db *gorm.DB
}

func NewApiKeyRepository(db *gorm.DB) ApiKeyRepository {
	return &apiKeyRepository{db: db}
}

func (r *apiKeyRepository) Create(ctx context.Context, apiKey model.ApiKey) (*model.ApiKey, error) {
	if err := r.db.WithContext(ctx).Create(&apiKey).Error; err != nil {
		return nil, err
	}

	return &apiKey, nil
}

func (r *apiKeyRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.ApiKey, error) {
	var apiKey model.ApiKey

	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&apiKey).Error; err != nil {
		return nil, err
	}

	return &apiKey, nil
}

func (r *apiKeyRepository) List(ctx context.Context, request model.ApiKeyListRequest) ([]*model.ApiKey, error) {
	var apiKeys []*model.ApiKey

	query := r.db.WithContext(ctx)

	// Filter by active status - default to active only
	if request.Active != "" {
		query = query.Where("is_active = ?", request.Active)
	} else {
		// Default: show only active API keys
		query = query.Where("is_active = ?", true)
	}

	// An empty scope lists every scope, so the settings screen renders both
	// sections in one call.
	if request.Scope != "" {
		query = query.Where("scope = ?", request.Scope)
	}

	if err := query.Offset((request.Page - 1) * request.PageSize).Limit(request.PageSize).Find(&apiKeys).Error; err != nil {
		return []*model.ApiKey{}, err
	}

	return apiKeys, nil
}

func (r *apiKeyRepository) Count(ctx context.Context, active string, scope string) (int64, error) {
	var count int64

	query := r.db.WithContext(ctx).Model(&model.ApiKey{})

	// Filter by active status - default to active only
	if active != "" {
		query = query.Where("is_active = ?", active)
	} else {
		// Default: count only active API keys
		query = query.Where("is_active = ?", true)
	}

	if scope != "" {
		query = query.Where("scope = ?", scope)
	}

	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

func (r *apiKeyRepository) Update(ctx context.Context, apiKey *model.ApiKey, isActive *bool, id uuid.UUID) (*model.ApiKey, error) {
	apiKey.UpdatedAt = time.Now()

	// Clearing the endpoint back to the provider default writes NULL, and GORM
	// skips nil pointers in a struct Updates — so an explicit clear would have
	// been a silent no-op, exactly like the deactivation toggle was. The
	// handler signals it with a non-nil BaseURLSet carrying a nil BaseURL.
	baseURLCleared := apiKey.BaseURLSet && apiKey.BaseURL == nil

	if err := r.db.WithContext(ctx).Where("id = ?", id).Updates(apiKey).Error; err != nil {
		return nil, err
	}

	if baseURLCleared {
		if err := r.db.WithContext(ctx).
			Model(&model.ApiKey{}).
			Where("id = ?", id).
			Update("base_url", nil).Error; err != nil {
			return nil, err
		}
	}

	// GORM's struct Updates skips zero values, so `false` could never travel
	// through the struct: an explicit column update is what makes deactivation
	// possible at all.
	if isActive != nil {
		if err := r.db.WithContext(ctx).
			Model(&model.ApiKey{}).
			Where("id = ?", id).
			Update("is_active", *isActive).Error; err != nil {
			return nil, err
		}
	}

	var updated model.ApiKey
	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&updated).Error; err != nil {
		return nil, err
	}

	return &updated, nil
}

// Delete removes the row. The model carries no DeletedAt, so this is a hard
// delete: the encrypted key leaves the database and the name is free again.
// Agents pointing at it keep existing (evo_core_agents.api_key_id is
// ON DELETE SET NULL). Returns false when no row matched.
func (r *apiKeyRepository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	result := r.db.WithContext(ctx).Where("id = ?", id).Delete(&model.ApiKey{})
	if result.Error != nil {
		return false, result.Error
	}

	return result.RowsAffected > 0, nil
}
