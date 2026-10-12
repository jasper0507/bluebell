package service

import (
	"context"
	"errors"
	"time"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
)

type CommentService struct {
	commentRepo *repository.CommentRepository
	postRepo    *repository.PostRepository
}

func NewCommentService(
	commentRepo *repository.CommentRepository,
	postRepo *repository.PostRepository,
) *CommentService {
	return &CommentService{
		commentRepo: commentRepo,
		postRepo:    postRepo,
	}
}

// CommentListItem 评论列表项
type CommentListItem struct {
	ID               uint
	Content          string
	AuthorID         string
	AuthorUsername   string
	ReplyToCommentID *uint
	CreatedAt        time.Time
}

var (
	ErrCommentNotFound    = repository.ErrCommentNotFound
	ErrInvalidReplyTarget = errors.New("无效的回复目标")
	ErrCommentForbidden   = errors.New("无权删除该评论")
)

// Create 创建评论
func (s *CommentService) Create(
	ctx context.Context,
	postID uint,
	authorID string,
	content string,
	replyToCommentID *uint,
) (uint, error) {
	// 1. 检查帖子是否存在
	exists, err := s.postRepo.ExistsByID(ctx, postID)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, ErrPostNotFound
	}

	// 2. 检查被回复评论是否存在，并核对帖子 ID 是否一致
	if replyToCommentID != nil {
		targetComment, err := s.commentRepo.FindByID(ctx, *replyToCommentID)

		if errors.Is(err, repository.ErrCommentNotFound) {
			return 0, ErrInvalidReplyTarget
		}

		if err != nil {
			return 0, err
		}

		if targetComment.PostID != postID {
			return 0, ErrInvalidReplyTarget
		}
	}

	// 3. 创建评论
	comment := &model.Comment{
		PostID:           postID,
		AuthorID:         authorID,
		ReplyToCommentID: replyToCommentID,
		Content:          content,
	}

	if err := s.commentRepo.Create(ctx, comment); err != nil {
		return 0, err
	}

	return comment.ID, nil
}

// Delete 删除评论
func (s *CommentService) Delete(
	ctx context.Context,
	commentID uint,
	userID string,
) error {
	// 1. 检查评论是否存在
	comment, err := s.commentRepo.FindByID(ctx, commentID)
	if err != nil {
		return err
	}

	// 2. 检查当前用户 ID 是否为评论作者
	if comment.AuthorID != userID {
		return ErrCommentForbidden
	}

	// 3. 软删除评论
	return s.commentRepo.Delete(ctx, commentID)
}

// List 获取帖子评论
func (s *CommentService) List(
	ctx context.Context,
	postID uint,
	page,
	pageSize int,
) ([]CommentListItem, int64, error) {
	offset, err := paginationOffset(page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	// 1. 检查帖子是否存在并统计评论数
	total, err := s.commentRepo.CountByPostID(ctx, postID)
	if err != nil {
		return nil, 0, err
	}

	if int64(offset) >= total {
		return []CommentListItem{}, total, nil
	}

	// 2. 分页查询评论及作者用户名
	comments, err := s.commentRepo.ListByPostID(ctx, postID, offset, pageSize)
	if err != nil {
		return nil, 0, err
	}

	data := make([]CommentListItem, 0, len(comments))
	for _, comment := range comments {
		if comment.AuthorUsername == nil {
			return nil, 0, repository.ErrUserNotFound
		}

		data = append(data, CommentListItem{
			ID:               comment.ID,
			Content:          comment.Content,
			AuthorID:         comment.AuthorID,
			AuthorUsername:   *comment.AuthorUsername,
			ReplyToCommentID: comment.ReplyToCommentID,
			CreatedAt:        comment.CreatedAt,
		})
	}

	return data, total, nil
}
