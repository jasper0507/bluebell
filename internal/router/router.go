package router

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/handler"
	"github.com/jasper0507/bluebell/internal/middleware"
)

// Dependencies 路由依赖
type Dependencies struct {
	UserHandler      *handler.UserHandler
	AuthHandler      *handler.AuthHandler
	CommunityHandler *handler.CommunityHandler
	PostHandler      *handler.PostHandler
	CommentHandler   *handler.CommentHandler

	AuthVerifier   middleware.AccessTokenVerifier
	TrustedProxies []string
	RateLimit      config.RateLimitConfig
}

// New 初始化路由
func New(deps Dependencies) (*gin.Engine, error) {
	r := gin.New()

	// 只信任配置的代理转发的客户端 IP，防止伪造 X-Forwarded-For 绕过限流
	if err := r.SetTrustedProxies(deps.TrustedProxies); err != nil {
		return nil, fmt.Errorf("设置可信代理失败: %w", err)
	}

	r.Use(
		middleware.RequestLogger(),
		gin.Recovery(),
	)

	authMiddleware := middleware.JWTAuth(deps.AuthVerifier)

	// 健康检查
	r.GET("/ping", handler.Ping)

	api := r.Group("/api/v1")

	// 限流关闭时，注册和登录路由挂空函数
	// 限流开启时给整个 /api/v1 分组挂上全局限流，authRateLimit 换成另一个更严格的限流实例
	authRateLimit := func(*gin.Context) {}
	if limit := deps.RateLimit; limit.Enabled {
		api.Use(middleware.RateLimit(rate.Limit(limit.API.Rate), limit.API.Burst))
		authRateLimit = middleware.RateLimit(rate.Limit(limit.Auth.Rate), limit.Auth.Burst)
	}

	api.POST("/signup", authRateLimit, deps.UserHandler.Register)
	api.POST("/login", authRateLimit, deps.AuthHandler.Login)
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

	return r, nil
}
