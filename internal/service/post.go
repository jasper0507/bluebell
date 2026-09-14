package service

import (
	"context"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
)

type PostService struct {
	PostRepo      *repository.PostRepository
	CommunityRepo *repository.CommunityRepository
}

func NewPostService(postRepository *repository.PostRepository, communityRepository *repository.CommunityRepository) *PostService {
	return &PostService{
		PostRepo:      postRepository,
		CommunityRepo: communityRepository,
	}
}

func (s *PostService) Create(ctx context.Context, title, content, authorID string, communityID uint) (uint, error) {
	// 1. 检查社区是否存在
	if _, err := s.CommunityRepo.FindByID(ctx, communityID); err != nil {
		return 0, err
	}

	// 2. 构建帖子
	post := &model.Post{
		Title:       title,
		Content:     content,
		AuthorID:    authorID,
		CommunityID: communityID,
	}

	// 3. 保存帖子
	if err := s.PostRepo.Create(ctx, post); err != nil {
		return 0, err
	}

	return post.ID, nil

}
