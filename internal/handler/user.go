package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/response"
	"github.com/jasper0507/bluebell/internal/service"
)

type UserHandler struct {
	userService *service.UserService
}

func NewUserHandler(userService *service.UserService) *UserHandler {
	return &UserHandler{
		userService: userService,
	}
}

// registerRequest 注册请求
type registerRequest struct {
	Username        string `json:"username" binding:"required,min=3,max=64,alphanum"`
	Password        string `json:"password" binding:"required,min=8,max=64,alphanum"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=Password"`
}

// Register 注册用户
func (h *UserHandler) Register(c *gin.Context) {
	// 1. 获取并检验参数
	var req registerRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 注册业务处理
	err := h.userService.Register(
		c.Request.Context(),
		req.Username,
		req.Password,
	)

	// 用户名已存在
	if errors.Is(err, service.ErrUsernameExists) {
		response.Error(c, response.CodeUsernameExists)
		return
	}

	// 注册失败
	if err != nil {
		slog.Error(
			"用户注册失败",
			"username", req.Username,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 3. 返回响应
	slog.Info("用户注册成功", "username", req.Username)
	response.Success(
		c,
		http.StatusCreated,
		gin.H{
			"username": req.Username,
		},
	)
}
