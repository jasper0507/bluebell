package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jasper0507/ripple/internal/cache"
	"github.com/jasper0507/ripple/internal/config"
	"github.com/jasper0507/ripple/internal/database"
	"github.com/jasper0507/ripple/internal/repository"
	"github.com/jasper0507/ripple/internal/store"
	"github.com/jasper0507/ripple/internal/worker"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 收到 Ctrl+C 或 SIGTERM 时取消重建
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// 初始化 MySQL
	db, err := database.Open(&cfg.MySQL)
	if err != nil {
		return fmt.Errorf("初始化 MySQL 失败: %w", err)
	}

	defer func() {
		if err := database.Close(db); err != nil {
			log.Printf("关闭 MySQL 失败: %v", err)
		}
	}()

	// 初始化 Redis
	rdb, err := cache.Open(ctx, &cfg.Redis)
	if err != nil {
		return fmt.Errorf("初始化 Redis 失败: %w", err)
	}

	defer func() {
		if err := cache.Close(rdb); err != nil {
			log.Printf("关闭 Redis 失败: %v", err)
		}
	}()

	// 全量重建帖子投影
	w := worker.NewOutboxWorker(
		repository.NewOutboxRepository(db),
		repository.NewPostRepository(db),
		store.NewPostStore(rdb),
	)

	start := time.Now()

	n, err := w.Rebuild(ctx)
	if err != nil {
		return fmt.Errorf("重建排行榜失败: %w", err)
	}

	log.Printf("排行榜重建完成：%d 篇帖子，耗时 %s", n, time.Since(start).Round(time.Millisecond))

	return nil
}
