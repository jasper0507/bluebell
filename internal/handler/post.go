package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
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

type createPostRequest struct {
	Title       string `json:"title" binding:"required,max=128"`
	Content     string `json:"content" binding:"required"`
	CommunityID uint   `json:"community_id" binding:"required"`
}

// postListItemResponse 帖子列表项
type postListItemResponse struct {
	ID            uint      `json:"id"`
	Title         string    `json:"title"`
	AuthorID      string    `json:"author_id"`
	AuthorName    string    `json:"author_name"`
	CommunityID   uint      `json:"community_id"`
	CommunityName string    `json:"community_name"`
	CreatedAt     time.Time `json:"created_at"`
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
	slog.Info("创建帖子成功", "post_id", postID,
		"author_id", authorID,
	)

	response.Success(c, http.StatusCreated, postID)
}

// Detail 获取帖子详情
func (h *PostHandler) Detail(c *gin.Context) {
	// 1. 获取并校验帖子id
	idstr := c.Param("id")

	id, err := strconv.ParseUint(idstr, 10, strconv.IntSize)
	if err != nil || id == 0 {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 查询帖子详情
	detail, err := h.postService.Detail(c.Request.Context(), uint(id))

	if errors.Is(err, service.ErrPostNotFound) {
		response.Error(c, response.CodePostNotFound)
		return
	}

	if err != nil {
		slog.Error("查询帖子详情失败", "post_id", id, "err", err)
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
			CreatedAt:     detail.Post.CreatedAt,
		},
		Content: detail.Post.Content,
	}
	// 4. 返回响应
	response.Success(c, http.StatusOK, data)
}

// List 获取帖子列表
func (h *PostHandler) List(c *gin.Context) {
	// 1. 获取并校验页码
	pageStr := c.DefaultQuery("page", "1")

	page, err := strconv.Atoi(pageStr)

	if err != nil || page < 1 {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 查询帖子列表
	posts, total, err := h.postService.List(c.Request.Context(), page, pageSize)

	if err != nil {
		slog.Error("查询帖子列表失败", "page", page, "err", err)
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
			CreatedAt:     post.CreatedAt,
		})
	}

	// 4. 返回响应
	response.Success(c, http.StatusOK, gin.H{
		"page":      page,
		"page_size": pageSize,
		"total":     total,
		"items":     items,
	})
}
