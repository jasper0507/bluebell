package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/middleware"
	"github.com/jasper0507/bluebell/internal/response"
	"github.com/jasper0507/bluebell/internal/service"
)

type PostHandler struct {
	PostService *service.PostService
}

func NewPostHandler(postService *service.PostService) *PostHandler {
	return &PostHandler{
		PostService: postService,
	}
}

type createPostRequest struct {
	Title       string `json:"title" binding:"required,max=128"`
	Content     string `json:"content" binding:"required"`
	CommunityID uint   `json:"community_id" binding:"required"`
}

// Create 创建帖子
func (h *PostHandler) Create(c *gin.Context) {
	// 1. 获取并校验参数
	var req createPostRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		slog.Warn("注册请求参数绑定失败", "err", err)
		response.Error(c, response.CodeInvalidParams)
		return
	}
	// 获取AuthorID
	authorID := c.GetString(middleware.ContextUserIDKey)

	// 2. 创建帖子
	postID, err := h.PostService.Create(
		c.Request.Context(),
		req.Title,
		req.Content,
		authorID,
		req.CommunityID,
	)

	if errors.Is(err, service.ErrCommunityNotFound) {
		slog.Warn("社区不存在", "err", err)
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
