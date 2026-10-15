package store

import (
	"testing"

	"github.com/jasper0507/ripple/internal/testenv"
	"github.com/redis/go-redis/v9"
)

// Redis DB 与其他测试包错开，见 testenv 包注释。
const testRedisDB = 15

var testRedis *redis.Client

func reset(t *testing.T) {
	t.Helper()
	testRedis = testenv.Redis(t, testRedisDB)
}
