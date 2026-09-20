package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jasper0507/bluebell/internal/model"
	"gorm.io/gorm"
)

type PostRepository struct {
	db *gorm.DB
}

func NewPostRepository(db *gorm.DB) *PostRepository {
	return &PostRepository{
		db: db,
	}
}

var ErrPostNotFound = errors.New("帖子不存在")

// Create 插入帖子
func (r *PostRepository) Create(ctx context.Context, post *model.Post) error {
	if err := gorm.G[model.Post](r.db).Create(ctx, post); err != nil {
		return fmt.Errorf("插入帖子失败: %w", err)
	}

	return nil
}

// FindByID 根据 ID 查询帖子
func (r *PostRepository) FindByID(ctx context.Context, id uint) (*model.Post, error) {
	post, err := gorm.G[model.Post](r.db).
		Where("id = ?", id).
		First(ctx)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPostNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("根据 ID 查询帖子失败: %w", err)
	}

	return &post, nil
}

// FindByIDs 根据ID列表批量查询帖子
func (r *PostRepository) FindByIDs(
	ctx context.Context,
	ids []uint,
) ([]model.Post, error) {
	posts, err := gorm.G[model.Post](r.db).
		Select("id, title, author_id, community_id, created_at").
		Where("id IN ?", ids).
		Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("根据ID列表查询帖子失败: %w", err)
	}

	return posts, nil
}

// Count 统计帖子总数
func (r *PostRepository) Count(ctx context.Context) (int64, error) {
	count, err := gorm.G[model.Post](r.db).Count(ctx, "*")

	if err != nil {
		return 0, fmt.Errorf("查询帖子总数失败: %w", err)
	}

	return count, nil
}
