package router

import (
	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/handler"
	"github.com/jasper0507/bluebell/internal/middleware"
	"github.com/jasper0507/bluebell/internal/service"
)

// Dependencies 路由依赖
type Dependencies struct {
	UserHandler      *handler.UserHandler
	CommunityHandler *handler.CommunityHandler
	PostHandler      *handler.PostHandler
	TokenService     *service.TokenService
}

// New 初始化路由
func New(deps Dependencies) *gin.Engine {
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
		users.POST("/login", deps.UserHandler.Login)
		users.POST("/register", deps.UserHandler.Register)
	}

	// 需要登录的接口
	authorized := api.Group("")
	authorized.Use(middleware.JWTAuth(deps.TokenService))
	{
		// 社区
		communities := authorized.Group("/communities")
		{
			communities.GET("", deps.CommunityHandler.List)
			communities.GET("/:id", deps.CommunityHandler.Detail)
		}

		// 帖子
		posts := authorized.Group("/posts")
		{
			posts.POST("", deps.PostHandler.Create)
			posts.GET("", deps.PostHandler.List)
			posts.GET("/:id", deps.PostHandler.Detail)
		}
	}

	return r
}
