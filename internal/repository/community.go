package repository

import (
	"context"
	"errors"
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

var ErrCommunityNotFound = errors.New("社区不存在")

// List 获取社区列表
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

// FindByID 根据ID查找社区
func (r *CommunityRepository) FindByID(ctx context.Context, id uint) (*model.Community, error) {
	community, err := gorm.G[model.Community](r.db).
		Select("*").
		Where("id=?", id).
		First(ctx)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCommunityNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("根据 ID 查询社区失败: %w", err)
	}

	return &community, nil
}
