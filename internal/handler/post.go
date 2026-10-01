package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/middleware"
	"github.com/jasper0507/bluebell/internal/response"
	"github.com/jasper0507/bluebell/internal/service"
)

type PostHandler struct {
	postService *service.PostService
}

func NewPostHandler(postService *service.PostService) *PostHandler {
	return &PostHandler{
		postService: postService,
	}
}

// createPostRequest 创建帖子请求
type createPostRequest struct {
	Title       string `json:"title" binding:"required,max=128"`
	Content     string `json:"content" binding:"required"`
	CommunityID uint   `json:"community_id" binding:"required"`
}

// postListRequest 帖子列表请求
type postListRequest struct {
	Page        int    `form:"page,default=1" binding:"min=1"`
	Order       string `form:"order,default=time"`
	CommunityID *uint  `form:"community_id" binding:"omitempty,min=1"`
}

// votePostRequest 投票请求
type votePostRequest struct {
	// 1: 赞成，0: 取消，-1: 反对
	Direction *int8 `json:"direction" binding:"required,oneof=-1 0 1"`
}

// postListItemResponse 帖子列表项
type postListItemResponse struct {
	ID            uint      `json:"id"`
	Title         string    `json:"title"`
	AuthorID      string    `json:"author_id"`
	AuthorName    string    `json:"author_name"`
	CommunityID   uint      `json:"community_id"`
	CommunityName string    `json:"community_name"`
	UpVotes       int64     `json:"up_votes"`
	DownVotes     int64     `json:"down_votes"`
	CreatedAt     time.Time `json:"created_at"`
}

// postListResponse 帖子列表
type postListResponse struct {
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Total    int64                  `json:"total"`
	Items    []postListItemResponse `json:"items"`
}

// postDetailResponse 帖子详情
type postDetailResponse struct {
	postListItemResponse
	Content string `json:"content"`
}

// pageSize 每页帖子数量
const pageSize = 10

// Create 创建帖子
func (h *PostHandler) Create(c *gin.Context) {
	// 1. 获取并校验参数
	var req createPostRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.CodeInvalidParams)
		return
	}
	// 获取AuthorID
	authorID := c.GetString(middleware.ContextUserIDKey)

	// 2. 创建帖子
	postID, err := h.postService.Create(
		c.Request.Context(),
		req.Title,
		req.Content,
		authorID,
		req.CommunityID,
	)

	if errors.Is(err, service.ErrCommunityNotFound) {
		response.Error(c, response.CodeCommunityNotFound)
		return
	}

	if err != nil {
		slog.Error(
			"创建帖子失败",
			"author_id", authorID,
			"community_id", req.CommunityID,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 3. 返回响应
	response.Success(c, http.StatusCreated, gin.H{
		"id": postID,
	})
}

// Delete 删除帖子
func (h *PostHandler) Delete(c *gin.Context) {
	// 1. 获取帖子ID
	postID, ok := parseUintParam(c, "postID")
	if !ok {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 获取当前userID
	userID := c.GetString(middleware.ContextUserIDKey)

	// 3. 删除帖子
	err := h.postService.Delete(c.Request.Context(), postID, userID)

	if errors.Is(err, service.ErrPostNotFound) {
		response.Error(c, response.CodePostNotFound)
		return
	}

	if errors.Is(err, service.ErrPostForbidden) {
		response.Error(c, response.CodeForbidden)
		return
	}

	if err != nil {
		slog.Error(
			"删除帖子失败",
			"post_id", postID,
			"user_id", userID,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 4. 返回响应
	slog.Info(
		"删除帖子成功",
		"post_id", postID,
		"user_id", userID,
	)

	c.Status(http.StatusNoContent)
}

// Detail 获取帖子详情
func (h *PostHandler) Detail(c *gin.Context) {
	// 1. 获取并校验帖子id
	postID, ok := parseUintParam(c, "postID")
	if !ok {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 查询帖子详情
	detail, err := h.postService.Detail(c.Request.Context(), postID)

	if errors.Is(err, service.ErrPostNotFound) {
		response.Error(c, response.CodePostNotFound)
		return
	}

	if err != nil {
		slog.Error("查询帖子详情失败", "post_id", postID, "err", err)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 3. 构建响应数据
	data := postDetailResponse{
		postListItemResponse: postListItemResponse{
			ID:            detail.Post.ID,
			Title:         detail.Post.Title,
			AuthorID:      detail.Post.AuthorID,
			AuthorName:    detail.AuthorName,
			CommunityID:   detail.Post.CommunityID,
			CommunityName: detail.CommunityName,
			UpVotes:       detail.UpVotes,
			DownVotes:     detail.DownVotes,
			CreatedAt:     detail.Post.CreatedAt,
		},
		Content: detail.Post.Content,
	}

	// 4. 返回响应
	response.Success(c, http.StatusOK, data)
}

// List 获取帖子列表
func (h *PostHandler) List(c *gin.Context) {
	// 1. 获取并校验参数
	var req postListRequest

	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 查询帖子列表
	posts, total, err := h.postService.List(
		c.Request.Context(),
		req.Page,
		pageSize,
		req.Order,
		req.CommunityID,
	)

	if errors.Is(err, service.ErrInvalidPostOrder) {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	if err != nil {
		slog.Error(
			"查询帖子列表失败",
			"page", req.Page,
			"order", req.Order,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 3. 构建响应数据
	items := make([]postListItemResponse, 0, len(posts))

	for _, post := range posts {
		items = append(items, postListItemResponse{
			ID:            post.ID,
			Title:         post.Title,
			AuthorID:      post.AuthorID,
			AuthorName:    post.AuthorName,
			CommunityID:   post.CommunityID,
			CommunityName: post.CommunityName,
			UpVotes:       post.UpVotes,
			DownVotes:     post.DownVotes,
			CreatedAt:     post.CreatedAt,
		})
	}

	// 4. 返回响应
	response.Success(c, http.StatusOK, postListResponse{
		Page:     req.Page,
		PageSize: pageSize,
		Total:    total,
		Items:    items,
	})
}

// Vote 投票
func (h *PostHandler) Vote(c *gin.Context) {
	// 1. 获取并校验帖子ID
	postID, ok := parseUintParam(c, "postID")
	if !ok {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 获取并校验投票方向
	req := votePostRequest{}

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 3. 获取投票用户ID
	userID := c.GetString(middleware.ContextUserIDKey)

	// 4. 执行投票
	err := h.postService.Vote(
		c.Request.Context(),
		userID,
		postID,
		*req.Direction,
	)

	if errors.Is(err, service.ErrPostNotFound) {
		response.Error(c, response.CodePostNotFound)
		return
	}

	if errors.Is(err, service.ErrVoteClosed) {
		response.Error(c, response.CodeVoteClosed)
		return
	}

	if err != nil {
		slog.Error(
			"帖子投票失败",
			"post_id", postID,
			"user_id", userID,
			"direction", *req.Direction,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 5. 返回响应
	response.Success(c, http.StatusOK, gin.H{
		"post_id":   postID,
		"direction": *req.Direction,
	})
}
