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
	ReplyToCommentID *uint  `json:"reply_to_comment_id" binding:"omitempty,min=1"`
}

// commentListItemResponse 评论列表项
type commentListItemResponse struct {
	ID               uint      `json:"id"`
	Content          string    `json:"content"`
	AuthorID         string    `json:"author_id"`
	AuthorName       string    `json:"author_name"`
	ReplyToCommentID *uint     `json:"reply_to_comment_id"`
	CreatedAt        time.Time `json:"created_at"`
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
	// 1. 获取帖子ID
	postIDStr := c.Param("id")

	postID, err := strconv.ParseUint(postIDStr, 10, strconv.IntSize)
	if err != nil || postID == 0 {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 获取帖子评论
	comments, err := h.commentService.List(c.Request.Context(), uint(postID))

	if errors.Is(err, service.ErrPostNotFound) {
		response.Error(c, response.CodePostNotFound)
		return
	}

	if err != nil {
		slog.Error(
			"查询帖子评论失败",
			"post_id", postID,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 3. 构建响应数据
	data := make([]commentListItemResponse, 0, len(comments))

	for _, comment := range comments {
		data = append(data, commentListItemResponse{
			ID:               comment.ID,
			Content:          comment.Content,
			AuthorID:         comment.AuthorID,
			AuthorName:       comment.AuthorName,
			ReplyToCommentID: comment.ReplyToCommentID,
			CreatedAt:        comment.CreatedAt,
		})
	}

	// 4. 返回响应
	response.Success(c, http.StatusOK, data)
}

// Delete 删除评论
func (h *CommentHandler) Delete(c *gin.Context) {

}
