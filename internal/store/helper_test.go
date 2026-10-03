package store

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jasper0507/bluebell/internal/cache"
	"github.com/jasper0507/bluebell/internal/config"
	"github.com/redis/go-redis/v9"
)

// 与 service 测试使用的 Redis DB 14 错开，go test 会并行跑这两个包。
const (
	testRedisAddr = "127.0.0.1:6379"
	testRedisDB   = 15
)

var (
	testRedis *redis.Client

	openOnce sync.Once
	openErr  error
)

func reset(t *testing.T) {
	t.Helper()

	openOnce.Do(func() {
		testRedis, openErr = openTestRedis()
	})
	if openErr != nil {
		t.Fatalf("集成测试需要先执行 make up 启动 Redis: %v", openErr)
	}

	if err := testRedis.FlushDB(t.Context()).Err(); err != nil {
		t.Fatalf("清空 Redis 测试库失败: %v", err)
	}
}

func openTestRedis() (*redis.Client, error) {
	conn, err := net.DialTimeout("tcp", testRedisAddr, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("Redis 未就绪: %w", err)
	}
	_ = conn.Close()

	return cache.Open(context.Background(), &config.RedisConfig{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
}
