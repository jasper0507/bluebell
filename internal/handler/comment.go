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

// commentListRequest 评论列表请求
type commentListRequest struct {
	Page int `form:"page,default=1" binding:"min=1"`
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

// commentListResponse 评论列表响应
type commentListResponse struct {
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
	Total    int64                     `json:"total"`
	Items    []commentListItemResponse `json:"items"`
}

// commentPageSize 每页评论数量
const commentPageSize = 10

// Create 创建评论
func (h *CommentHandler) Create(c *gin.Context) {
	// 1. 获取帖子id和用户id
	postID, ok := parseUintParam(c, "postID")
	if !ok {
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
		postID,
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
	// 1. 获取帖子 ID
	postID, ok := parseUintParam(c, "postID")
	if !ok {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 获取并校验分页参数
	var req commentListRequest

	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 3. 获取帖子评论
	comments, total, err := h.commentService.List(
		c.Request.Context(),
		postID,
		req.Page,
		commentPageSize,
	)

	if errors.Is(err, service.ErrPostNotFound) {
		response.Error(c, response.CodePostNotFound)
		return
	}

	if err != nil {
		slog.Error(
			"查询帖子评论失败",
			"post_id", postID,
			"page", req.Page,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 4. 构建响应数据
	items := make([]commentListItemResponse, 0, len(comments))

	for _, comment := range comments {
		items = append(items, commentListItemResponse(comment))
	}

	// 5. 返回响应
	response.Success(c, http.StatusOK, commentListResponse{
		Page:     req.Page,
		PageSize: commentPageSize,
		Total:    total,
		Items:    items,
	})
}

// Delete 删除评论
func (h *CommentHandler) Delete(c *gin.Context) {
	// 1. 获取评论 ID
	commentID, ok := parseUintParam(c, "commentID")
	if !ok {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 获取当前用户 ID
	userID := c.GetString(middleware.ContextUserIDKey)

	// 3. 删除评论
	err := h.commentService.Delete(
		c.Request.Context(),
		uint(commentID),
		userID,
	)

	if errors.Is(err, service.ErrCommentNotFound) {
		response.Error(c, response.CodeCommentNotFound)
		return
	}

	if errors.Is(err, service.ErrCommentForbidden) {
		response.Error(c, response.CodeForbidden)
		return
	}

	if err != nil {
		slog.Error(
			"删除评论失败",
			"comment_id", commentID,
			"user_id", userID,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 4. 返回响应
	c.Status(http.StatusNoContent)
}
