package app

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/handler"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/router"
	"github.com/jasper0507/bluebell/internal/service"
	"github.com/redis/go-redis/v9"

	"gorm.io/gorm"
)

// New 组装应用依赖
func New(db *gorm.DB, rdb *redis.Client, jwtCfg *config.JWTConfig) (*gin.Engine, error) {
	// 初始化 Token 服务
	tokenService, err := service.NewTokenService(
		jwtCfg.Secret,
		jwtCfg.Issuer,
		jwtCfg.AccessTokenTTL,
	)
	if err != nil {
		return nil, fmt.Errorf("初始化 JWT 服务失败: %w", err)
	}

	// 用户模块
	userHandler := newUserHandler(db, tokenService)

	// 社区模块
	communityHandler := newCommunityHandler(db)

	// 帖子模块
	postHandler := newPostHandler(db, rdb)

	// 初始化路由
	r := router.New(router.Dependencies{
		UserHandler:      userHandler,
		CommunityHandler: communityHandler,
		PostHandler:      postHandler,
		TokenService:     tokenService,
	})

	return r, nil
}

// newUserHandler 组装用户模块依赖
func newUserHandler(
	db *gorm.DB,
	tokenService *service.TokenService,
) *handler.UserHandler {
	userRepo := repository.NewUserRepository(db)
	userService := service.NewUserService(userRepo, tokenService)

	return handler.NewUserHandler(userService)
}

// newCommunityHandler 组装社区模块依赖
func newCommunityHandler(db *gorm.DB) *handler.CommunityHandler {
	communityRepo := repository.NewCommunityRepository(db)
	communityService := service.NewCommunityService(communityRepo)

	return handler.NewCommunityHandler(communityService)
}

// newPostHandler 组装帖子模块依赖
func newPostHandler(
	db *gorm.DB,
	rdb *redis.Client,
) *handler.PostHandler {
	postRepo := repository.NewPostRepository(db)
	userRepo := repository.NewUserRepository(db)
	communityRepo := repository.NewCommunityRepository(db)
	postRedisRepo := repository.NewPostRedisRepository(rdb)

	postService := service.NewPostService(
		postRepo,
		userRepo,
		communityRepo,
		postRedisRepo,
	)

	return handler.NewPostHandler(postService)
}
