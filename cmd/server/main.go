package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jasper0507/bluebell/internal/app"
	"github.com/jasper0507/bluebell/internal/cache"
	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/database"
	applog "github.com/jasper0507/bluebell/internal/logger"
	"github.com/jasper0507/bluebell/internal/server"
	"golang.org/x/sync/errgroup"
)

func main() {
	if err := run(); err != nil {
		// 不用 log.Fatal：SetDefault 后 log 输出会经过 slog，级别固定为 INFO
		slog.Error("服务运行失败", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// 1. 加载配置
	cfg, err := config.Load()

	// 配置加载失败
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 2. 创建日志记录器
	logger, err := applog.New(&cfg.Log)

	// 日志记录器初始化失败
	if err != nil {
		return fmt.Errorf("初始化日志失败: %w", err)
	}
	slog.SetDefault(logger)

	// 3. 初始化数据库连接
	db, err := database.Open(&cfg.MySQL)

	// 数据库连接初始化失败
	if err != nil {
		return fmt.Errorf("初始化 MySQL 失败: %w", err)
	}

	defer func() {
		if err := database.Close(db); err != nil {
			slog.Warn("关闭 MySQL 失败", "error", err)
		}
	}()

	slog.Info(
		"MySQL 初始化完成",
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
		return fmt.Errorf("初始化 Redis 失败: %w", err)
	}

	defer func() {
		if err := cache.Close(rdb); err != nil {
			slog.Warn("关闭 Redis 失败", "error", err)
		}
	}()

	slog.Info("Redis 初始化完成", "address", cfg.Redis.Addr)

	// 5. 组装应用依赖
	application, err := app.New(
		db,
		rdb,
		cfg,
	)
	if err != nil {
		return fmt.Errorf("初始化应用失败: %w", err)
	}

	// 6. 创建进程生命周期上下文
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	g, ctx := errgroup.WithContext(ctx)

	// 7. 启动 HTTP Server
	g.Go(func() error {
		slog.Info(
			"启动 HTTP 服务",
			"address", cfg.HTTP.Addr,
		)

		if err := server.Run(
			ctx,
			application.Router,
			&cfg.HTTP,
		); err != nil {
			return fmt.Errorf("运行 HTTP 服务失败: %w", err)
		}

		return nil
	})

	// 8. 启动 Outbox Worker
	g.Go(func() error {
		slog.Info("启动 Outbox Worker")

		if err := application.OutboxWorker.Run(ctx); err != nil {
			return fmt.Errorf("运行 Outbox Worker 失败: %w", err)
		}

		return nil
	})

	return g.Wait()
}
