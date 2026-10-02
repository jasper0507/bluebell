package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const refreshTokenKeyPrefix = "bluebell:auth:refresh:"

type RefreshTokenStore struct {
	rdb *redis.Client
}

func NewRefreshTokenStore(rdb *redis.Client) *RefreshTokenStore {
	return &RefreshTokenStore{
		rdb: rdb,
	}
}

// refreshTokenKey 生成一个基于 SHA-256 的 Refresh Token 键
func refreshTokenKey(token string) string {
	// 对 token 进行 SHA-256 哈希
	sum := sha256.Sum256([]byte(token))

	return refreshTokenKeyPrefix + hex.EncodeToString(sum[:])
}

// Save 保存 Refresh Token 到 Redis
func (s *RefreshTokenStore) Save(
	ctx context.Context,
	token,
	userID string,
	ttl time.Duration,
) error {
	if err := s.rdb.Set(
		ctx,
		refreshTokenKey(token),
		userID,
		ttl,
	).Err(); err != nil {
		return fmt.Errorf("保存 Refresh Token 失败: %w", err)
	}

	return nil
}
