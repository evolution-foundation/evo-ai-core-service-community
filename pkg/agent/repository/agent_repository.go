package repository

import (
	"context"
	"evo-ai-core-service/pkg/agent/model"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AgentRepository interface {
	Create(ctx context.Context, agent model.Agent) (*model.Agent, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.Agent, error)
	List(ctx context.Context, page int, pageSize int, filters []model.AgentListFilter, search string) ([]*model.Agent, error)
	Update(ctx context.Context, agent *model.Agent, id uuid.UUID) (*model.Agent, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
	Count(ctx context.Context, filters []model.AgentListFilter, search string) (int64, error)
	CountByFolderID(ctx context.Context, folderId uuid.UUID) (int64, error)
	RemoveFolder(ctx context.Context, id uuid.UUID) (*model.Agent, error)
	ListAgentsByFolderID(ctx context.Context, folderId uuid.UUID, page int, pageSize int) ([]*model.Agent, error)
}

type agentRepository struct {
	db *gorm.DB
}

func NewAgentRepository(db *gorm.DB) AgentRepository {
	return &agentRepository{db: db}
}

func (r *agentRepository) Create(ctx context.Context, agent model.Agent) (*model.Agent, error) {
	if err := r.db.WithContext(ctx).Create(&agent).Error; err != nil {
		return nil, err
	}

	return &agent, nil
}

func (r *agentRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Agent, error) {
	var agent model.Agent

	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&agent).Error; err != nil {
		return nil, err
	}

	return &agent, nil
}

func (r *agentRepository) List(ctx context.Context, page int, pageSize int, filters []model.AgentListFilter, search string) ([]*model.Agent, error) {
	var agents []*model.Agent

	query := applyAgentFilters(r.db.WithContext(ctx), filters, search)
	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&agents).Error; err != nil {
		return []*model.Agent{}, err
	}

	return agents, nil
}

func (r *agentRepository) Update(ctx context.Context, agent *model.Agent, id uuid.UUID) (*model.Agent, error) {
	agent.UpdatedAt = time.Now()
	if err := r.db.WithContext(ctx).Where("id = ?", id).Updates(agent).First(&agent).Error; err != nil {
		return nil, err
	}

	return agent, nil
}

func (r *agentRepository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	if err := r.db.WithContext(ctx).Model(&model.Agent{}).Where("id = ?", id).Delete(&model.Agent{}).Error; err != nil {
		return false, err
	}

	return true, nil
}

func (r *agentRepository) Count(ctx context.Context, filters []model.AgentListFilter, search string) (int64, error) {
	var count int64
	query := applyAgentFilters(r.db.WithContext(ctx).Model(&model.Agent{}), filters, search)
	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

func (r *agentRepository) CountByFolderID(ctx context.Context, folderId uuid.UUID) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Agent{}).Where("folder_id = ?", folderId).Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

func (r *agentRepository) RemoveFolder(ctx context.Context, id uuid.UUID) (*model.Agent, error) {
	var agent model.Agent
	if err := r.db.WithContext(ctx).Model(&model.Agent{}).Where("id = ?", id).Update("folder_id", nil).First(&agent).Error; err != nil {
		return nil, err
	}

	return &agent, nil
}

func (r *agentRepository) ListAgentsByFolderID(ctx context.Context, folderId uuid.UUID, page int, pageSize int) ([]*model.Agent, error) {
	var agents []*model.Agent

	query := r.db.WithContext(ctx).Where("folder_id = ?", folderId)

	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&agents).Error; err != nil {
		return []*model.Agent{}, err
	}

	return agents, nil
}
