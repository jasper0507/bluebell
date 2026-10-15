package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jasper0507/ripple/internal/model"
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

// CommentListData 评论列表数据
type CommentListData struct {
	ID               uint
	Content          string
	AuthorID         string
	AuthorUsername   *string
	ReplyToCommentID *uint
	CreatedAt        time.Time
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

// CountByPostID 检查帖子存在并统计评论数量，帖子不存在时返回 ErrPostNotFound
func (r *CommentRepository) CountByPostID(
	ctx context.Context,
	postID uint,
) (int64, error) {
	total, err := gorm.G[int64](r.db).Raw(`
    SELECT (
        SELECT COUNT(*)
        FROM comments c
        WHERE c.post_id = p.id AND c.deleted_at IS NULL
    )
    FROM posts p
    WHERE p.id = ? AND p.deleted_at IS NULL
	`, postID).Take(ctx)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, ErrPostNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("统计帖子评论数量失败: %w", err)
	}

	return total, nil
}

// ListByPostID 根据帖子 ID 分页查询评论及作者用户名
func (r *CommentRepository) ListByPostID(
	ctx context.Context,
	postID uint,
	offset,
	limit int,
) ([]CommentListData, error) {
	comments, err := gorm.G[CommentListData](r.db).Raw(`
        SELECT
            c.id,
            c.content,
            c.author_id,
            u.username AS author_username,
            c.reply_to_comment_id,
            c.created_at
        FROM comments c
        LEFT JOIN users u ON u.user_id = c.author_id
        WHERE c.post_id = ?
          AND c.deleted_at IS NULL
        ORDER BY c.created_at, c.id
        LIMIT ? OFFSET ?
    `, postID, limit, offset).Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("查询帖子评论失败: %w", err)
	}

	return comments, nil
}
