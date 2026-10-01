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

// Delete 软删除评论
func (r *CommentRepository) Delete(ctx context.Context, id uint) error {
	_, err := gorm.G[model.Comment](r.db).
		Where("id = ?", id).
		Delete(ctx)

	if err != nil {
		return fmt.Errorf("软删除评论失败: %w", err)
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

// CountByPostID 统计帖子评论数量
func (r *CommentRepository) CountByPostID(
	ctx context.Context,
	postID uint,
) (int64, error) {
	total, err := gorm.G[model.Comment](r.db).
		Where("post_id = ?", postID).
		Count(ctx, "*")
	if err != nil {
		return 0, fmt.Errorf("统计帖子评论数量失败: %w", err)
	}

	return total, nil
}

// ListByPostID 根据帖子 ID 分页查询评论
func (r *CommentRepository) ListByPostID(
	ctx context.Context,
	postID uint,
	offset,
	limit int,
) ([]model.Comment, error) {
	comments, err := gorm.G[model.Comment](r.db).
		Select("id, content, author_id, reply_to_comment_id, created_at").
		Where("post_id = ?", postID).
		Order("created_at ASC, id ASC").
		Offset(offset).
		Limit(limit).
		Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("查询帖子评论失败: %w", err)
	}

	return comments, nil
}
