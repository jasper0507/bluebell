package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenManager_ParseAccessTokenRejectsInvalid(t *testing.T) {
	const (
		secret = "12345678901234567890123456789012"
		issuer = "ripple-test"
	)
	manager, err := NewTokenManager(secret, issuer, time.Hour, time.Hour)
	if err != nil {
		t.Fatalf("创建 TokenManager 失败: %v", err)
	}

	valid, err := manager.generateAccessToken("user-1")
	if err != nil {
		t.Fatalf("生成访问令牌失败: %v", err)
	}
	if userID, err := manager.ParseAccessToken(valid); err != nil || userID != "user-1" {
		t.Fatalf("有效令牌解析为 %q, err = %v", userID, err)
	}

	now := time.Now()
	claims := func(mutate func(*jwt.RegisteredClaims)) *jwt.RegisteredClaims {
		c := &jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   "user-1",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		}
		mutate(c)
		return c
	}
	sign := func(method jwt.SigningMethod, key any, c *jwt.RegisteredClaims) string {
		token, err := jwt.NewWithClaims(method, c).SignedString(key)
		if err != nil {
			t.Fatalf("签名失败: %v", err)
		}
		return token
	}

	for _, tt := range []struct {
		name  string
		token string
	}{
		{name: "其他密钥签名", token: sign(jwt.SigningMethodHS256, []byte("abcdefghijklmnopqrstuvwxyz123456"), claims(func(*jwt.RegisteredClaims) {}))},
		{name: "非 HS256 算法", token: sign(jwt.SigningMethodHS512, []byte(secret), claims(func(*jwt.RegisteredClaims) {}))},
		{name: "none 算法", token: sign(jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType, claims(func(*jwt.RegisteredClaims) {}))},
		{name: "已过期", token: sign(jwt.SigningMethodHS256, []byte(secret), claims(func(c *jwt.RegisteredClaims) {
			c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))
		}))},
		{name: "缺少过期时间", token: sign(jwt.SigningMethodHS256, []byte(secret), claims(func(c *jwt.RegisteredClaims) { c.ExpiresAt = nil }))},
		{name: "issuer 不符", token: sign(jwt.SigningMethodHS256, []byte(secret), claims(func(c *jwt.RegisteredClaims) { c.Issuer = "other" }))},
		{name: "缺少用户标识", token: sign(jwt.SigningMethodHS256, []byte(secret), claims(func(c *jwt.RegisteredClaims) { c.Subject = "" }))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if userID, err := manager.ParseAccessToken(tt.token); err == nil {
				t.Fatalf("应当拒绝，实际解析为 %q", userID)
			}
		})
	}
}
