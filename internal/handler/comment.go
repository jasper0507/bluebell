package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/middleware"
	"github.com/jasper0507/bluebell/internal/response"
	"github.com/jasper0507/bluebell/internal/service"
)

type CommentHandler struct {
	commentService *service.CommentService
}

func NewCommentHandler(commentService *service.CommentService) *CommentHandler {
	return &CommentHandler{
		commentService: commentService,
	}
}

// createCommentRequest 创建评论请求
type createCommentRequest struct {
	Content          string `json:"content" binding:"required"`
	ReplyToCommentID *uint  `json:"reply_to_id" binding:"omitempty,min=1"`
}

// Create 创建评论
func (h *CommentHandler) Create(c *gin.Context) {
	// 1. 获取帖子id和用户id
	postIDStr := c.Param("id")

	postID, err := strconv.ParseUint(postIDStr, 10, strconv.IntSize)
	if err != nil || postID == 0 {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	userID := c.GetString(middleware.ContextUserIDKey)

	// 2. 获取请求参数
	var req createCommentRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 3. 创建评论
	commentID, err := h.commentService.Create(
		c.Request.Context(),
		uint(postID),
		userID,
		req.Content,
		req.ReplyToCommentID,
	)

	if errors.Is(err, service.ErrPostNotFound) {
		response.Error(c, response.CodePostNotFound)
		return
	}

	if errors.Is(err, service.ErrCommentNotFound) {
		response.Error(c, response.CodeCommentNotFound)
		return
	}

	if errors.Is(err, service.ErrInvalidReplyTarget) {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	if err != nil {
		slog.Error(
			"创建评论失败",
			"post_id", postID,
			"user_id", userID,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 4. 返回响应
	response.Success(c, http.StatusCreated, gin.H{
		"id": commentID,
	})

}

// List 获取帖子评论
func (h *CommentHandler) List(c *gin.Context) {

}

// Delete 删除评论
func (h *CommentHandler) Delete(c *gin.Context) {

}
