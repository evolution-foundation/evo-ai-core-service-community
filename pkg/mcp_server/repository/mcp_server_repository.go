package repository

import (
	"context"
	"evo-ai-core-service/pkg/mcp_server/model"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type McpServerRepository interface {
	Create(ctx context.Context, mcpServer model.McpServer) (*model.McpServer, error)
	GetByID(ctx context.Context, id uuid.UUID) (*model.McpServer, error)
	List(ctx context.Context, page int, pageSize int, search string) ([]*model.McpServer, error)
	Count(ctx context.Context, search string) (int64, error)
	Update(ctx context.Context, mcpServer *model.McpServer, id uuid.UUID) (*model.McpServer, error)
	Delete(ctx context.Context, id uuid.UUID) (bool, error)
}

type mcpServerRepository struct {
	db *gorm.DB
}

func NewMcpServerRepository(db *gorm.DB) McpServerRepository {
	return &mcpServerRepository{db: db}
}

func (r *mcpServerRepository) Create(ctx context.Context, mcpServer model.McpServer) (*model.McpServer, error) {
	if err := r.db.WithContext(ctx).Create(&mcpServer).Error; err != nil {
		return nil, err
	}

	return &mcpServer, nil
}

func (r *mcpServerRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.McpServer, error) {
	var mcpServer model.McpServer

	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&mcpServer).Error; err != nil {
		return nil, err
	}

	return &mcpServer, nil
}

func (r *mcpServerRepository) List(ctx context.Context, page int, pageSize int, search string) ([]*model.McpServer, error) {
	var mcpServer []*model.McpServer

	query := applyMcpServerSearch(r.db.WithContext(ctx), search)

	if err := query.Offset((page - 1) * pageSize).Limit(pageSize).Find(&mcpServer).Error; err != nil {
		return []*model.McpServer{}, err
	}

	return mcpServer, nil
}

func (r *mcpServerRepository) Count(ctx context.Context, search string) (int64, error) {
	var count int64

	query := applyMcpServerSearch(r.db.WithContext(ctx).Model(&model.McpServer{}), search)

	if err := query.Count(&count).Error; err != nil {
		return 0, err
	}

	return count, nil
}

// List and Count share the clause so the total counts only the rows the search keeps.
func applyMcpServerSearch(query *gorm.DB, search string) *gorm.DB {
	if search == "" {
		return query
	}
	return query.Where("name ILIKE ?", "%"+search+"%")
}

func (r *mcpServerRepository) Update(ctx context.Context, mcpServer *model.McpServer, id uuid.UUID) (*model.McpServer, error) {
	mcpServer.UpdatedAt = time.Now()
	if err := r.db.WithContext(ctx).Where("id = ?", id).Updates(mcpServer).First(&mcpServer).Error; err != nil {
		return nil, err
	}

	return mcpServer, nil
}

func (r *mcpServerRepository) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	if err := r.db.WithContext(ctx).Model(&model.McpServer{}).Where("id = ?", id).Delete(&model.McpServer{}).Error; err != nil {
		return false, err
	}

	return true, nil
}
