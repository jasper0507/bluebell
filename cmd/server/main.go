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
	applog "github.com/jasper0507/bluebell/internal/logger"
	"github.com/jasper0507/bluebell/internal/router"
	"github.com/jasper0507/bluebell/internal/server"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// 1. 加载配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// 2. 创建日志记录器
	logger, err := applog.New(&cfg.Log)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	slog.SetDefault(logger)

	// 3. 初始化数据库连接
	db, err := database.Open(&cfg.MySQL)
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
		"max_open_conns", cfg.MySQL.MaxOpenConns,
		"max_idle_conns", cfg.MySQL.MaxIdleConns,
		"conn_max_lifetime", cfg.MySQL.ConnMaxLifetime,
	)

	// 4. 初始化 Redis
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)

	rdb, err := cache.Open(ctx, &cfg.Redis)

	cancel()

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
	r := router.New()

	// 6. 启动 HTTP 服务
	slog.Info("starting HTTP server", "address", cfg.HTTP.Addr)
	if err := server.Run(r, &cfg.HTTP); err != nil {
		return fmt.Errorf("run HTTP server: %w", err)
	}
	return nil
}
