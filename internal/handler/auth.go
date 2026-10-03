package handler

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/response"
	"github.com/jasper0507/bluebell/internal/service"
)

type AuthHandler struct {
	authService  *service.AuthService
	cookieSecure bool
}

func NewAuthHandler(
	authService *service.AuthService,
	cookieSecure bool,
) *AuthHandler {
	return &AuthHandler{
		authService:  authService,
		cookieSecure: cookieSecure,
	}
}

// accessTokenResponse 访问令牌响应
type accessTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

// loginRequest 登录请求
type loginRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=64"`
}

// loginResponse 登录响应
type loginResponse struct {
	Username string `json:"username"`
	accessTokenResponse
}

const refreshTokenCookieName = "refresh_token"

func (h *AuthHandler) setRefreshTokenCookie(
	c *gin.Context,
	token string,
	ttl time.Duration,
) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     refreshTokenCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
		HttpOnly: true,
		Secure:   h.cookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
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

	// 3. 设置刷新令牌 cookie
	h.setRefreshTokenCookie(
		c,
		tokens.RefreshToken,
		tokens.RefreshTokenTTL,
	)

	// 4. 返回响应
	slog.Info(
		"用户登录成功",
		"username", req.Username,
	)

	response.Success(c, http.StatusOK, loginResponse{
		Username: req.Username,
		accessTokenResponse: accessTokenResponse{
			AccessToken: tokens.AccessToken,
			TokenType:   "Bearer",
		},
	})
}

// Refresh 刷新令牌
func (h *AuthHandler) Refresh(c *gin.Context) {
	// 1. 获取 Refresh Token
	refreshToken, err := c.Cookie(refreshTokenCookieName)
	if err != nil {
		response.Error(c, response.CodeUnauthorized)
		return
	}

	// 2. 刷新令牌
	tokens, err := h.authService.Refresh(
		c.Request.Context(),
		refreshToken,
	)

	if errors.Is(err, service.ErrInvalidRefreshToken) {
		response.Error(c, response.CodeUnauthorized)
		return
	}

	if err != nil {
		slog.Error("刷新令牌失败", "err", err)
		response.Error(c, response.CodeInternalError)
		return
	}

	// 3. 更新 Refresh Token Cookie
	h.setRefreshTokenCookie(
		c,
		tokens.RefreshToken,
		tokens.RefreshTokenTTL,
	)

	// 4. 返回新的 Access Token
	response.Success(c, http.StatusOK, accessTokenResponse{
		AccessToken: tokens.AccessToken,
		TokenType:   "Bearer",
	})
}

// Logout 退出登录
func (h *AuthHandler) Logout(c *gin.Context) {
	// TODO
}
