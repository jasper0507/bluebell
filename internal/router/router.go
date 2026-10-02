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

	api := r.Group("/api/v1")
	auth := middleware.JWTAuth(deps.AccessTokenService)

	// 用户
	users := api.Group("/users")
	{
		users.POST("/login", deps.UserHandler.Login)
		users.POST("/register", deps.UserHandler.Register)
	}

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
		posts.POST("", auth, deps.PostHandler.Create)
		posts.DELETE("/:postID", auth, deps.PostHandler.Delete)

		posts.GET("/:postID/vote", auth, deps.PostHandler.GetVote)
		posts.PUT("/:postID/vote", auth, deps.PostHandler.Vote)

		posts.POST("/:postID/comments", auth, deps.CommentHandler.Create)
	}

	// 评论
	comments := api.Group("/comments")
	{
		comments.DELETE("/:commentID", auth, deps.CommentHandler.Delete)
	}

	return r
}
