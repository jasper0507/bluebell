package service

import (
	"context"
	"errors"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
)

type CommentService struct {
	commentRepo *repository.CommentRepository
	postRepo    *repository.PostRepository
	userRepo    *repository.UserRepository
}

func NewCommentService(
	commentRepo *repository.CommentRepository,
	postRepo *repository.PostRepository,
	userRepo *repository.UserRepository,
) *CommentService {
	return &CommentService{
		commentRepo: commentRepo,
		postRepo:    postRepo,
		userRepo:    userRepo,
	}
}

var (
	ErrInvalidReplyTarget = errors.New("无效的回复目标")
	ErrCommentNotFound    = repository.ErrCommentNotFound
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
	if _, err := s.postRepo.FindByID(ctx, postID); err != nil {
		return 0, err
	}

	// 2. 检查被回复评论是否存在，并核对帖子id是否一致
	if replyToCommentID != nil {
		target, err := s.commentRepo.FindByID(ctx, *replyToCommentID)
		if err != nil {
			return 0, err
		}

		if target.PostID != postID {
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
