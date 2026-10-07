package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RefreshTokenStore struct {
	rdb *redis.Client
}

func NewRefreshTokenStore(rdb *redis.Client) *RefreshTokenStore {
	return &RefreshTokenStore{
		rdb: rdb,
	}
}

// refreshTokenKeyPrefix 是刷新令牌 Redis 键的前缀。
// 键由前缀和令牌的 SHA-256 哈希组成，String 值保存用户 ID。
const refreshTokenKeyPrefix = "bluebell:auth:refresh:"

var ErrRefreshTokenNotFound = fmt.Errorf("Refresh Token 不存在")

// rotateRefreshTokenScript 原子轮换刷新令牌
//
// KEYS[1]: 旧刷新令牌的 Redis 键
// KEYS[2]: 新刷新令牌的 Redis 键
//
// 返回值
// 成功时返回数组：[1] 为用户 ID，[2] 为剩余 TTL（毫秒）。
// 令牌不存在或 TTL 非正时返回空数组，Rotate 将其映射为 ErrRefreshTokenNotFound。
var rotateRefreshTokenScript = redis.NewScript(`
-- 获取用户ID
local userID = redis.call('GET', KEYS[1])
if not userID then
	return {}
end

-- 获取当前剩余 TTL，单位毫秒
local ttl = redis.call('PTTL', KEYS[1])
if ttl <= 0 then
	redis.call('DEL', KEYS[1])
	return {}
end

-- 设置新 key，并继承剩余 TTL
redis.call('SET', KEYS[2], userID, 'PX', ttl)

-- 删除旧 key
redis.call('DEL', KEYS[1])

return {userID, ttl}
`)

// Save 保存刷新令牌
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

// Delete 删除刷新令牌
func (s *RefreshTokenStore) Delete(
	ctx context.Context,
	token string,
) error {
	if err := s.rdb.Del(
		ctx,
		refreshTokenKey(token),
	).Err(); err != nil {
		return fmt.Errorf("删除 Refresh Token 失败: %w", err)
	}

	return nil
}

// Rotate 原子轮换刷新令牌，并继承原刷新令牌的剩余有效期
func (s *RefreshTokenStore) Rotate(
	ctx context.Context,
	oldToken,
	newToken string,
) (string, time.Duration, error) {
	// 执行原子脚本，获取用户 ID 和剩余 TTL
	result, err := rotateRefreshTokenScript.Run(
		ctx,
		s.rdb,
		[]string{
			refreshTokenKey(oldToken),
			refreshTokenKey(newToken),
		},
	).Slice()

	if err != nil {
		return "", 0, fmt.Errorf("轮换 Refresh Token 失败: %w", err)
	}

	if len(result) == 0 {
		return "", 0, ErrRefreshTokenNotFound
	}

	userID := result[0].(string)
	ttl := result[1].(int64)

	return userID, time.Duration(ttl) * time.Millisecond, nil
}

// refreshTokenKey 生成一个基于 SHA-256 的刷新令牌键
func refreshTokenKey(token string) string {
	// 对 token 进行 SHA-256 哈希
	sum := sha256.Sum256([]byte(token))

	return refreshTokenKeyPrefix + hex.EncodeToString(sum[:])
}
