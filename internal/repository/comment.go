package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jasper0507/bluebell/internal/model"
	"gorm.io/gorm"
)

type CommentRepository struct {
	db *gorm.DB
}

func NewCommentRepository(db *gorm.DB) *CommentRepository {
	return &CommentRepository{
		db: db,
	}
}

var ErrCommentNotFound = errors.New("评论不存在")

// Create 创建评论
func (r *CommentRepository) Create(ctx context.Context, comment *model.Comment) error {
	if err := gorm.G[model.Comment](r.db).Create(ctx, comment); err != nil {
		return fmt.Errorf("插入评论失败: %w", err)
	}

	return nil
}

// FindByID 根据 ID 查询评论
func (r *CommentRepository) FindByID(ctx context.Context, id uint) (*model.Comment, error) {
	comment, err := gorm.G[model.Comment](r.db).
		Where("id = ?", id).
		First(ctx)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCommentNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("根据 ID 查询评论失败: %w", err)
	}

	return &comment, nil
}
