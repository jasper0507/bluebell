package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
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

type registerRequest struct {
	Username        string `json:"username" binding:"required,min=3,max=64"`
	Password        string `json:"password" binding:"required,min=8,max=64"`
	ConfirmPassword string `json:"confirm_password" binding:"required,eqfield=Password"`
}

type loginRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=64"`
}

// Register 注册用户
func (h *UserHandler) Register(c *gin.Context) {
	// 1. 获取并检验参数
	var req registerRequest

	// 参数绑定失败
	if err := c.ShouldBindJSON(&req); err != nil {
		slog.Warn("Register ", "err", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"msg": "invalid request",
		})
		return
	}

	// 2. 注册业务处理
	err := h.userService.Register(
		c.Request.Context(),
		req.Username,
		req.Password,
	)

	if err != nil {
		slog.Error("Register ", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "register failed",
		})
		return
	}

	// 3. 返回响应
	slog.Info("Register ", "username", req.Username)
	c.JSON(http.StatusCreated, gin.H{
		"msg":      "register success",
		"username": req.Username,
	})
}

func (h *UserHandler) Login(c *gin.Context) {
	// 1. 获取并检验参数
	var req loginRequest

	// 参数绑定失败
	if err := c.ShouldBindJSON(req); err != nil {
		slog.Warn("Login ", "err", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"msg": "invalid request",
		})
		return
	}
	// 2. 登录业务处理
	userID, err := h.userService.Login(c.Request.Context(), req.Username, req.Password)

	if errors.Is(err, service.ErrInvalidCredentials) {
		c.JSON(http.StatusUnauthorized, gin.H{
			"msg": "用户名或密码错误",
		})
	}

	if err != nil {
		slog.Error(
			"登录失败",
			"username", req.Username,
			"err", err,
		)
		c.JSON(http.StatusInternalServerError, gin.H{
			"msg": "服务器内部错误",
		})
		return
	}

	// 3. 返回响应
	slog.Info("Login ", "user_id", userID, "username", req.Username)
	c.JSON(http.StatusOK, gin.H{
		"msg":      "login success",
		"user_id":  userID,
		"username": req.Username,
	})

}
