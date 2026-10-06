package app

import (
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/handler"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/router"
	"github.com/jasper0507/bluebell/internal/service"
	"github.com/jasper0507/bluebell/internal/store"
	"github.com/jasper0507/bluebell/internal/worker"
)

type App struct {
	Router       *gin.Engine
	OutboxWorker *worker.OutboxWorker
}

// New 组装应用依赖
func New(
	db *gorm.DB,
	rdb *redis.Client,
	cfg *config.Config,
) (*App, error) {
	tokenManager, err := service.NewTokenManager(
		cfg.Auth.Secret,
		cfg.Auth.Issuer,
		cfg.Auth.AccessTokenTTL,
		cfg.Auth.RefreshTokenTTL,
	)
	if err != nil {
		return nil, err
	}

	authHandler := newAuthHandler(
		db,
		rdb,
		tokenManager,
		cfg.Auth.CookieSecure,
	)
	userHandler := newUserHandler(db)
	communityHandler := newCommunityHandler(db)
	postHandler := newPostHandler(db, rdb)
	commentHandler := newCommentHandler(db)

	r := router.New(router.Dependencies{
		UserHandler:      userHandler,
		AuthHandler:      authHandler,
		CommunityHandler: communityHandler,
		PostHandler:      postHandler,
		CommentHandler:   commentHandler,
		AuthVerifier:     tokenManager,
	})

	return &App{
		Router:       r,
		OutboxWorker: newOutboxWorker(db, rdb),
	}, nil
}

// newOutboxWorker 组装 Outbox Worker 依赖
func newOutboxWorker(
	db *gorm.DB,
	rdb *redis.Client,
) *worker.OutboxWorker {
	outboxRepo := repository.NewOutboxRepository(db)
	postRepo := repository.NewPostRepository(db)
	postStore := store.NewPostStore(rdb)

	return worker.NewOutboxWorker(
		outboxRepo,
		postRepo,
		postStore,
	)
}

// newAuthHandler 组装认证模块依赖
func newAuthHandler(
	db *gorm.DB,
	rdb *redis.Client,
	tokenManager *service.TokenManager,
	cookieSecure bool,
) *handler.AuthHandler {
	userRepo := repository.NewUserRepository(db)
	refreshTokenStore := store.NewRefreshTokenStore(rdb)
	authService := service.NewAuthService(userRepo, refreshTokenStore, tokenManager)

	return handler.NewAuthHandler(
		authService,
		cookieSecure,
	)
}

// newUserHandler 组装用户模块依赖
func newUserHandler(db *gorm.DB) *handler.UserHandler {
	userRepo := repository.NewUserRepository(db)
	userService := service.NewUserService(userRepo)

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
	postStore := store.NewPostStore(rdb)

	postService := service.NewPostService(
		postRepo,
		userRepo,
		communityRepo,
		postStore,
	)

	return handler.NewPostHandler(postService)
}

// newCommentHandler 组装评论模块依赖
func newCommentHandler(db *gorm.DB) *handler.CommentHandler {
	commentRepo := repository.NewCommentRepository(db)
	postRepo := repository.NewPostRepository(db)
	userRepo := repository.NewUserRepository(db)

	commentService := service.NewCommentService(
		commentRepo,
		postRepo,
		userRepo,
	)

	return handler.NewCommentHandler(commentService)
}
