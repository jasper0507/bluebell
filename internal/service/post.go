package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jasper0507/ripple/internal/model"
	"github.com/jasper0507/ripple/internal/repository"
	"github.com/jasper0507/ripple/internal/store"
)

type PostService struct {
	postRepo      *repository.PostRepository
	communityRepo *repository.CommunityRepository
	postStore     *store.PostStore
}

func NewPostService(
	postRepo *repository.PostRepository,
	communityRepo *repository.CommunityRepository,
	postStore *store.PostStore,
) *PostService {
	return &PostService{
		postRepo:      postRepo,
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

	// 2. 检查当前用户 ID 是否为帖子作者
	if userID != post.AuthorID {
		return ErrPostForbidden
	}

	// 3. 软删除帖子
	return s.postRepo.Delete(ctx, postID)
}

// Detail 获取帖子详情
func (s *PostService) Detail(
	ctx context.Context,
	postID uint,
) (*PostDetail, error) {
	// 1. 查询帖子、作者及社区
	detail, err := s.postRepo.FindDetailByID(ctx, postID)
	if err != nil {
		return nil, err
	}

	// 帖子必须有作者和社区，LEFT JOIN 查不到说明数据不一致
	if detail.AuthorUsername == nil || detail.CommunityName == nil {
		return nil, fmt.Errorf("帖子 %d 缺少作者或社区数据", postID)
	}

	// 2. 查询实时投票统计
	upVotes, downVotes, err := s.postRepo.CountVotesByPostID(ctx, postID)
	if err != nil {
		return nil, err
	}

	// 3. 构建并返回帖子详情
	return &PostDetail{
		Post:           &detail.Post,
		AuthorUsername: *detail.AuthorUsername,
		CommunityName:  *detail.CommunityName,
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
	// 1. 校验排序方式，未指定时按发布时间排序
	if order == "" {
		order = store.PostRankTime
	}
	if order != store.PostRankTime && order != store.PostRankHot {
		return nil, 0, ErrInvalidPostOrder
	}

	// 2. 从 Redis 获取当前页排好序的帖子 ID 和总数
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

	// 3. 批量查询帖子及其关联信息
	posts, err := s.postRepo.FindListByIDs(ctx, postIDs)
	if err != nil {
		return nil, 0, err
	}

	// 4. 按 Redis 排名恢复顺序
	postMap := make(map[uint]repository.PostListData, len(posts))
	for _, post := range posts {
		postMap[post.ID] = post
	}

	data := make([]PostListItem, 0, len(posts))
	for _, id := range postIDs {
		post, ok := postMap[id]
		// 已删除但 Redis 投影尚未同步的帖子，直接跳过
		if !ok {
			continue
		}

		if post.AuthorUsername == nil || post.CommunityName == nil {
			return nil, 0, fmt.Errorf("帖子 %d 缺少作者或社区数据", post.ID)
		}

		data = append(data, PostListItem{
			ID:             post.ID,
			Title:          post.Title,
			AuthorID:       post.AuthorID,
			AuthorUsername: *post.AuthorUsername,
			CommunityID:    post.CommunityID,
			CommunityName:  *post.CommunityName,
			CreatedAt:      post.CreatedAt,
		})
	}

	return data, total, nil
}

// Vote 投票
func (s *PostService) Vote(
	ctx context.Context,
	postID uint,
	userID string,
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
	postID uint,
	userID string,
) (int8, error) {
	// 1. 检查帖子是否存在
	exists, err := s.postRepo.ExistsByID(ctx, postID)
	if err != nil {
		return 0, err
	}
	if !exists {
		return 0, ErrPostNotFound
	}

	// 2. 查询用户投票状态
	return s.postRepo.FindUserVote(
		ctx,
		postID,
		userID,
	)
}
