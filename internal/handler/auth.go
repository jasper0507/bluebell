package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/response"
	"github.com/jasper0507/bluebell/internal/service"
)

type AuthHandler struct {
	authService *service.AuthService
}

func NewAuthHandler(authService *service.AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

// loginRequest 登录请求
type loginRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=64"`
}

// loginResponse 登录响应
type loginResponse struct {
	Username     string `json:"username"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
}

// Login 用户登录
func (h *AuthHandler) Login(c *gin.Context) {
	// 1. 获取并检验参数
	var req loginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		slog.Warn("登录请求参数绑定失败", "err", err)
		response.Error(c, response.CodeInvalidParams)
		return
	}

	// 2. 登录业务处理
	tokens, err := h.authService.Login(
		c.Request.Context(),
		req.Username,
		req.Password,
	)

	// 用户名或密码错误
	if errors.Is(err, service.ErrInvalidCredentials) {
		response.Error(c, response.CodeInvalidCredentials)
		return
	}

	// 登录失败
	if err != nil {
		slog.Error(
			"用户登录失败",
			"username", req.Username,
			"err", err,
		)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 3. 返回响应
	slog.Info(
		"用户登录成功",
		"username", req.Username,
	)
	response.Success(c, http.StatusOK, loginResponse{
		Username:     req.Username,
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		TokenType:    "Bearer",
	})
}

// Refresh 刷新令牌
func (h *AuthHandler) Refresh(c *gin.Context) {
	// TODO
}

// Logout 退出登录
func (h *AuthHandler) Logout(c *gin.Context) {
	// TODO
}
