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

// CommentListItem 评论列表项
type CommentListItem struct {
	ID               uint
	Content          string
	AuthorID         string
	AuthorName       string
	ReplyToCommentID *uint
	CreatedAt        time.Time
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

// List 获取帖子评论
func (s *CommentService) List(ctx context.Context, postID uint) ([]CommentListItem, error) {
	// 1. 检查帖子是否存在
	if _, err := s.postRepo.FindByID(ctx, postID); err != nil {
		return nil, err
	}

	// 2. 获取评论
	comments, err := s.commentRepo.ListByPostID(ctx, postID)
	if err != nil {
		return nil, err
	}

	if len(comments) == 0 {
		return []CommentListItem{}, nil
	}

	// 3. 获取评论作者ID
	authorIDs := make([]string, 0, len(comments))
	for _, comment := range comments {
		authorIDs = append(authorIDs, comment.AuthorID)
	}

	// 4. 批量查询评论作者名字
	authorNames, err := s.userRepo.FindNamesByUserIDs(ctx, authorIDs)
	if err != nil {
		return nil, err
	}

	// 5. 构建评论列表并返回
	data := make([]CommentListItem, 0, len(comments))

	for _, comment := range comments {
		data = append(data, CommentListItem{
			ID:               comment.ID,
			Content:          comment.Content,
			AuthorID:         comment.AuthorID,
			AuthorName:       authorNames[comment.AuthorID],
			ReplyToCommentID: comment.ReplyToCommentID,
			CreatedAt:        comment.CreatedAt,
		})
	}

	return data, nil
}
