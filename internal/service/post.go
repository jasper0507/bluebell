package service

import (
	"context"
	"time"

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

// PostDetail 帖子详情
type PostDetail struct {
	Post          *model.Post
	AuthorName    string
	CommunityName string
}

// PostListItem 帖子列表项
type PostListItem struct {
	ID            uint
	Title         string
	AuthorID      string
	AuthorName    string
	CommunityID   uint
	CommunityName string
	CreatedAt     time.Time
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

// List 列出帖子
func (s *PostService) List(ctx context.Context, page, pageSize int) ([]PostListItem, int64, error) {
	// 1. 获取帖子总数
	total, err := s.postRepo.Count(ctx)

	if err != nil {
		return nil, 0, err
	}

	// 2. 分页查询帖子
	offset := (page - 1) * pageSize

	posts, err := s.postRepo.FindPage(ctx, offset, pageSize)

	if err != nil {
		return nil, 0, err
	}

	// 3. 批量获取AuthorID和CommunityID
	authorIDs := make([]string, 0, len(posts))
	communityIDs := make([]uint, 0, len(posts))

	for _, post := range posts {
		authorIDs = append(authorIDs, post.AuthorID)
		communityIDs = append(communityIDs, post.CommunityID)
	}

	// 4. 批量获取AuthorName和CommunityName
	authorNames, err := s.userRepo.FindNamesByUserIDs(ctx, authorIDs)

	if err != nil {
		return nil, 0, err
	}

	communityNames, err := s.communityRepo.FindNamesByIDs(ctx, communityIDs)

	if err != nil {
		return nil, 0, err
	}

	// 5. 构建并返回帖子列表
	data := make([]PostListItem, 0, len(posts))

	for _, post := range posts {
		data = append(data, PostListItem{
			ID:            post.ID,
			Title:         post.Title,
			AuthorID:      post.AuthorID,
			AuthorName:    authorNames[post.AuthorID],
			CommunityID:   post.CommunityID,
			CommunityName: communityNames[post.CommunityID],
			CreatedAt:     post.CreatedAt,
		})
	}

	return data, total, nil
}
