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
func (s *CommentService) List(
	ctx context.Context,
	postID uint,
	page,
	pageSize int,
) ([]CommentListItem, int64, error) {
	// 1. 检查帖子是否存在
	if _, err := s.postRepo.FindByID(ctx, postID); err != nil {
		return nil, 0, err
	}

	// 2. 获取评论总数
	total, err := s.commentRepo.CountByPostID(ctx, postID)
	if err != nil {
		return nil, 0, err
	}

	if total == 0 {
		return []CommentListItem{}, total, nil
	}

	// 3. 分页查询评论
	offset := (page - 1) * pageSize

	comments, err := s.commentRepo.ListByPostID(
		ctx,
		postID,
		offset,
		pageSize,
	)
	if err != nil {
		return nil, 0, err
	}

	// 4. 获取评论作者 ID
	authorIDs := make([]string, 0, len(comments))

	for _, comment := range comments {
		authorIDs = append(authorIDs, comment.AuthorID)
	}

	// 5. 批量查询评论作者名字
	authorNames, err := s.userRepo.FindNamesByUserIDs(ctx, authorIDs)
	if err != nil {
		return nil, 0, err
	}

	// 6. 构建评论列表
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

	return data, total, nil
}
