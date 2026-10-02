package router

import (
	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/handler"
	"github.com/jasper0507/bluebell/internal/middleware"
	"github.com/jasper0507/bluebell/internal/service"
)

// Dependencies 路由依赖
type Dependencies struct {
	UserHandler        *handler.UserHandler
	AuthHandler        *handler.AuthHandler
	CommunityHandler   *handler.CommunityHandler
	PostHandler        *handler.PostHandler
	CommentHandler     *handler.CommentHandler
	AccessTokenService *service.AccessTokenService
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

	authMiddleware := middleware.JWTAuth(deps.AccessTokenService)

	api := r.Group("/api/v1")

	api.POST("/signup", deps.UserHandler.Register)
	api.POST("/login", deps.AuthHandler.Login)
	api.POST("/refresh", deps.AuthHandler.Refresh)
	api.POST("/logout", deps.AuthHandler.Logout)

	// 社区
	communities := api.Group("/communities")
	{
		communities.GET("", deps.CommunityHandler.List)
		communities.GET("/:communityID", deps.CommunityHandler.Detail)
	}

	// 帖子
	posts := api.Group("/posts")
	{
		// 公开接口
		posts.GET("", deps.PostHandler.List)
		posts.GET("/:postID", deps.PostHandler.Detail)
		posts.GET("/:postID/comments", deps.CommentHandler.List)

		// 登录接口
		protected := posts.Group("")
		protected.Use(authMiddleware)
		{
			protected.POST("", deps.PostHandler.Create)
			protected.DELETE("/:postID", deps.PostHandler.Delete)

			protected.GET("/:postID/vote", deps.PostHandler.GetVote)
			protected.PUT("/:postID/vote", deps.PostHandler.Vote)

			protected.POST("/:postID/comments", deps.CommentHandler.Create)
		}
	}

	// 评论
	comments := api.Group("/comments")
	{
		comments.DELETE("/:commentID", authMiddleware, deps.CommentHandler.Delete)
	}

	return r
}
