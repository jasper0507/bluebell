package repository

import (
	"context"
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

func (r *PostRepository) Create(ctx context.Context, post *model.Post) error {
	if err := gorm.G[model.Post](r.db).Create(ctx, post); err != nil {
		return fmt.Errorf("插入帖子失败: %w", err)
	}

	return nil
}
