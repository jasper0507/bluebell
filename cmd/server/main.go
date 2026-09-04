package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/jasper0507/bluebell/internal/cache"
	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/database"
	"github.com/jasper0507/bluebell/internal/handler"
	applog "github.com/jasper0507/bluebell/internal/logger"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/router"
	"github.com/jasper0507/bluebell/internal/server"
	"github.com/jasper0507/bluebell/internal/service"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// 1. 加载配置
	cfg, err := config.Load()

	// 配置加载失败
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. 创建日志记录器
	logger, err := applog.New(&cfg.Log)

	// 日志记录器初始化失败
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	slog.SetDefault(logger)

	// 3. 初始化数据库连接
	db, err := database.Open(&cfg.MySQL)

	// 数据库连接初始化失败
	if err != nil {
		return fmt.Errorf("init MySQL: %w", err)
	}

	defer func() {
		if err := database.Close(db); err != nil {
			slog.Warn("close MySQL", "error", err)
		}
	}()

	slog.Info(
		"MySQL initialized",
		"host", cfg.MySQL.Host,
		"port", cfg.MySQL.Port,
		"max_open_conns", cfg.MySQL.MaxOpenConns,
		"max_idle_conns", cfg.MySQL.MaxIdleConns,
		"conn_max_lifetime", cfg.MySQL.ConnMaxLifetime,
	)

	// 4. 初始化 Redis
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	rdb, err := cache.Open(ctx, &cfg.Redis)

	cancel()

	// Redis 初始化失败
	if err != nil {
		return fmt.Errorf("init Redis: %w", err)
	}

	defer func() {
		if err := cache.Close(rdb); err != nil {
			slog.Warn("close Redis", "error", err)
		}
	}()

	slog.Info("Redis initialized", "address", cfg.Redis.Addr)

	// 5. 注册路由
	userRepo := repository.NewUserRepository(db)
	userService := service.NewUserService(userRepo)
	userHandler := handler.NewUserHandler(userService)

	r := router.New(userHandler)

	// 6. 启动 HTTP 服务
	slog.Info("starting HTTP server", "address", cfg.HTTP.Addr)
	if err := server.Run(r, &cfg.HTTP); err != nil {
		return fmt.Errorf("run HTTP server: %w", err)
	}
	return nil
}
