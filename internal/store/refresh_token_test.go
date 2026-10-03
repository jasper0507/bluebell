package store

import (
	"errors"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRefreshToken_RotateInheritsRemainingTTL(t *testing.T) {
	reset(t)

	ctx := t.Context()
	tokens := NewRefreshTokenStore(testRedis)
	const (
		userID = "user-1"
		old    = "refresh-old"
		ttl    = 10 * time.Second
	)

	if err := tokens.Save(ctx, old, userID, ttl); err != nil {
		t.Fatalf("保存失败: %v", err)
	}
	if err := testRedis.Get(ctx, "bluebell:auth:refresh:"+old).Err(); !errors.Is(err, redis.Nil) {
		t.Fatal("Redis 保存了 refresh token 原文")
	}
	if got, err := testRedis.Get(ctx, refreshTokenKey(old)).Result(); err != nil || got != userID {
		t.Fatalf("哈希键中的用户 = %q, err = %v", got, err)
	}

	gotUserID, gotTTL, err := tokens.Rotate(ctx, old, "refresh-new")
	if err != nil {
		t.Fatalf("轮换失败: %v", err)
	}
	if gotUserID != userID {
		t.Fatalf("userID = %q, want %q", gotUserID, userID)
	}
	if gotTTL <= 9*time.Second || gotTTL > ttl {
		t.Fatalf("剩余有效期 = %s, want 在 9s 和 %s 之间", gotTTL, ttl)
	}
	if err := testRedis.Get(ctx, refreshTokenKey(old)).Err(); !errors.Is(err, redis.Nil) {
		t.Fatal("轮换后旧 token 仍存在")
	}
	if got, err := testRedis.Get(ctx, refreshTokenKey("refresh-new")).Result(); err != nil || got != userID {
		t.Fatalf("新 token 的用户 = %q, err = %v", got, err)
	}

	if _, _, err := tokens.Rotate(ctx, old, "refresh-other"); !errors.Is(err, ErrRefreshTokenNotFound) {
		t.Fatalf("旧 token 再次轮换 err = %v", err)
	}
	if err := tokens.Delete(ctx, "refresh-new"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if _, _, err := tokens.Rotate(ctx, "refresh-new", "refresh-next"); !errors.Is(err, ErrRefreshTokenNotFound) {
		t.Fatalf("删除后轮换 err = %v", err)
	}
}
