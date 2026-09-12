package router

import (
	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/handler"
	"github.com/jasper0507/bluebell/internal/middleware"
	"github.com/jasper0507/bluebell/internal/service"
)

// New 初始化路由
func New(
	userHandler *handler.UserHandler,
	communityHandler *handler.CommunityHandler,
	tokenService *service.TokenService,
) *gin.Engine {
	r := gin.New()

	r.Use(
		middleware.RequestLogger(),
		gin.Recovery(),
	)

	// 健康检查
	r.GET("/ping", handler.Ping)
	r.GET("/health", handler.Healthz)

	api := r.Group("/api/v1")

	// 用户
	users := api.Group("/users")
	{
		users.POST("/login", userHandler.Login)
		users.POST("/register", userHandler.Register)
	}

	// 需要登录的接口
	authorized := api.Group("")
	authorized.Use(middleware.JWTAuth(tokenService))
	{
		// 社区
		communities := authorized.Group("/communities")
		{
			communities.GET("", communityHandler.List)
		}
	}

	return r
}
