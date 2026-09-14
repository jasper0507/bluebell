package service

import (
	"context"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
)

type PostService struct {
	postRepo      *repository.PostRepository
	userRepo      *repository.UserRepository
	communityRepo *repository.CommunityRepository
}

func NewPostService(postRepository *repository.PostRepository, userRepository *repository.UserRepository, communityRepository *repository.CommunityRepository) *PostService {
	return &PostService{
		postRepo:      postRepository,
		userRepo:      userRepository,
		communityRepo: communityRepository,
	}
}

type PostDetail struct {
	Post          *model.Post
	AuthorName    string
	CommunityName string
}

var ErrPostNotFound = repository.ErrPostNotFound

// Create 创建帖子
func (s *PostService) Create(ctx context.Context, title, content, authorID string, communityID uint) (uint, error) {
	// 1. 检查社区是否存在
	if _, err := s.communityRepo.FindByID(ctx, communityID); err != nil {
		return 0, err
	}

	// 2. 构建帖子
	post := &model.Post{
		Title:       title,
		Content:     content,
		AuthorID:    authorID,
		CommunityID: communityID,
	}

	// 3. 插入帖子
	if err := s.postRepo.Create(ctx, post); err != nil {
		return 0, err
	}

	return post.ID, nil

}

// Detail 获取帖子详情
func (s *PostService) Detail(ctx context.Context, id uint) (*PostDetail, error) {
	// 1. 获取帖子
	post, err := s.postRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// 2. 获取作者
	author, err := s.userRepo.FindByUserID(ctx, post.AuthorID)
	if err != nil {
		return nil, err
	}

	// 3. 获取社区
	community, err := s.communityRepo.FindByID(ctx, post.CommunityID)
	if err != nil {
		return nil, err
	}

	// 4. 构建并返回帖子详情
	return &PostDetail{
		Post:          post,
		AuthorName:    author.Username,
		CommunityName: community.Name,
	}, nil

}
