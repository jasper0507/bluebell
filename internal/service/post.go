package service

import (
	"context"
	"errors"
	"time"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/store"
)

type PostService struct {
	postRepo      *repository.PostRepository
	userRepo      *repository.UserRepository
	communityRepo *repository.CommunityRepository
	postStore     *store.PostStore
}

func NewPostService(
	postRepo *repository.PostRepository,
	userRepo *repository.UserRepository,
	communityRepo *repository.CommunityRepository,
	postStore *store.PostStore,
) *PostService {
	return &PostService{
		postRepo:      postRepo,
		userRepo:      userRepo,
		communityRepo: communityRepo,
		postStore:     postStore,
	}
}

// PostDetail 帖子详情
type PostDetail struct {
	Post           *model.Post
	AuthorUsername string
	CommunityName  string
	UpVotes        int64
	DownVotes      int64
}

// PostListItem 帖子列表项
type PostListItem struct {
	ID             uint
	Title          string
	AuthorID       string
	AuthorUsername string
	CommunityID    uint
	CommunityName  string
	CreatedAt      time.Time
}

const (
	PostOrderByHot  = "hot"
	PostOrderByTime = "time"
)

var (
	ErrPostNotFound     = repository.ErrPostNotFound
	ErrInvalidPostOrder = errors.New("无效的排序方式")
	ErrPostForbidden    = errors.New("无权删除该帖子")
)

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

// Delete 删除帖子
func (s *PostService) Delete(ctx context.Context, postID uint, userID string) error {
	// 1. 检查帖子是否存在
	post, err := s.postRepo.FindByID(ctx, postID)
	if err != nil {
		return err
	}

	// 2. 检查当前userID是否为帖子作者
	if userID != post.AuthorID {
		return ErrPostForbidden
	}

	// 3. 软删除帖子
	return s.postRepo.Delete(ctx, postID)
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

	// 4. 获取投票统计
	upVotes, downVotes, err := s.postRepo.CountVotesByPostID(ctx, post.ID)
	if err != nil {
		return nil, err
	}

	// 5. 构建并返回帖子详情
	return &PostDetail{
		Post:           post,
		AuthorUsername: author.Username,
		CommunityName:  community.Name,
		UpVotes:        upVotes,
		DownVotes:      downVotes,
	}, nil
}

// List 获取帖子列表
func (s *PostService) List(
	ctx context.Context,
	page,
	pageSize int,
	order string,
	communityID *uint,
) ([]PostListItem, int64, error) {
	// 1. 校验排序方式
	if order != PostOrderByTime && order != PostOrderByHot {
		return nil, 0, ErrInvalidPostOrder
	}

	// 2. 从 Redis 获取当前页排好序的帖子ID和总数
	offset, err := paginationOffset(page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	postIDs, total, err := s.postStore.FindPostIDs(
		ctx,
		communityID,
		order,
		offset,
		pageSize,
	)
	if err != nil {
		return nil, 0, err
	}

	// 3. 根据帖子ID批量查询 MySQL
	posts, err := s.postRepo.FindByIDs(ctx, postIDs)
	if err != nil {
		return nil, 0, err
	}

	if len(posts) == 0 {
		return []PostListItem{}, total, nil
	}

	// 恢复帖子排序
	posts = orderPostsByIDs(posts, postIDs)

	// 4. 收集作者ID和社区ID
	authorIDs := make([]string, 0, len(posts))
	communityIDs := make([]uint, 0, len(posts))

	for _, post := range posts {
		authorIDs = append(authorIDs, post.AuthorID)
		communityIDs = append(communityIDs, post.CommunityID)
	}

	// 5. 批量查询作者名、社区名和投票统计
	authorUsernames, err := s.userRepo.FindUsernamesByUserIDs(
		ctx,
		authorIDs,
	)
	if err != nil {
		return nil, 0, err
	}

	communityNames, err := s.communityRepo.FindNamesByIDs(
		ctx,
		communityIDs,
	)
	if err != nil {
		return nil, 0, err
	}

	// 6. 构建帖子列表
	data := make([]PostListItem, 0, len(posts))

	for _, post := range posts {
		data = append(data, PostListItem{
			ID:             post.ID,
			Title:          post.Title,
			AuthorID:       post.AuthorID,
			AuthorUsername: authorUsernames[post.AuthorID],
			CommunityID:    post.CommunityID,
			CommunityName:  communityNames[post.CommunityID],
			CreatedAt:      post.CreatedAt,
		})
	}

	return data, total, nil
}

// orderPostsByIDs 按 Redis 返回的ID顺序重新排列帖子
func orderPostsByIDs(
	posts []model.Post,
	ids []uint,
) []model.Post {
	postMap := make(map[uint]model.Post, len(posts))

	for _, post := range posts {
		postMap[post.ID] = post
	}

	ordered := make([]model.Post, 0, len(posts))

	for _, id := range ids {
		if post, ok := postMap[id]; ok {
			ordered = append(ordered, post)
		}
	}

	return ordered
}

// Vote 投票
func (s *PostService) Vote(
	ctx context.Context,
	userID string,
	postID uint,
	direction int8,
) error {
	return s.postRepo.Vote(
		ctx,
		postID,
		userID,
		direction,
	)
}

// GetVote 获取用户对帖子的投票状态
func (s *PostService) GetVote(
	ctx context.Context,
	userID string,
	postID uint,
) (int8, error) {
	// 1. 检查帖子是否存在
	if _, err := s.postRepo.FindByID(ctx, postID); err != nil {
		return 0, err
	}

	// 2. 查询用户投票状态
	return s.postRepo.FindUserVote(
		ctx,
		postID,
		userID,
	)
}
