package repository

import (
	"context"
	"fmt"

	"github.com/jasper0507/bluebell/internal/model"

	"gorm.io/gorm"
)

type CommunityRepository struct {
	db *gorm.DB
}

func NewCommunityRepository(db *gorm.DB) *CommunityRepository {
	return &CommunityRepository{
		db: db,
	}
}

func (r *CommunityRepository) List(ctx context.Context) ([]model.Community, error) {
	communities, err := gorm.G[model.Community](r.db).
		Select("id", "name").
		Order("id").
		Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("查询社区列表失败: %w", err)
	}

	return communities, nil
}
