package cache

import (
	"context"
	"fmt"

	"github.com/jasper0507/bluebell/internal/config"
	"github.com/redis/go-redis/v9"
)

// 创建 Redis 客户端，并检查 Redis 是否可用
func Open(ctx context.Context, cfg *config.RedisConfig) (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// 通过 PING 实际访问 Redis，确认连接和认证均正常
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}

	return rdb, nil
}

// Close 关闭 Redis 客户端及其连接池
func Close(rdb *redis.Client) error {
	if rdb == nil {
		return nil
	}

	if err := rdb.Close(); err != nil {
		return fmt.Errorf("close Redis: %w", err)
	}

	return nil
}
