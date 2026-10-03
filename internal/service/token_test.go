package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	testSecret = "12345678901234567890123456789012"
	testIssuer = "bluebell-test"
	testUserID = "019-test-user-id"
)

func TestNewTokenManager(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		issuer  string
		access  time.Duration
		refresh time.Duration
	}{
		{name: "secret 不足 32 字节", secret: testSecret[:31], issuer: testIssuer, access: time.Hour, refresh: time.Hour},
		{name: "issuer 为空", secret: testSecret, issuer: "", access: time.Hour, refresh: time.Hour},
		{name: "access ttl 不是正数", secret: testSecret, issuer: testIssuer, access: 0, refresh: time.Hour},
		{name: "refresh ttl 不是正数", secret: testSecret, issuer: testIssuer, access: time.Hour, refresh: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewTokenManager(tt.secret, tt.issuer, tt.access, tt.refresh); err == nil {
				t.Fatal("应当返回错误")
			}
		})
	}
}

func TestTokenManager_AccessTokenRoundTrip(t *testing.T) {
	const accessTTL = 15 * time.Minute

	manager, err := NewTokenManager(testSecret, testIssuer, accessTTL, 24*time.Hour)
	if err != nil {
		t.Fatalf("创建 TokenManager 失败: %v", err)
	}

	token, err := manager.generateAccessToken(testUserID)
	if err != nil {
		t.Fatalf("生成 Access Token 失败: %v", err)
	}

	userID, err := manager.ParseAccessToken(token)
	if err != nil {
		t.Fatalf("解析 Access Token 失败: %v", err)
	}
	if userID != testUserID {
		t.Fatalf("userID = %q, want %q", userID, testUserID)
	}

	claims := &jwt.RegisteredClaims{}
	if _, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return []byte(testSecret), nil
	}); err != nil {
		t.Fatalf("读取声明失败: %v", err)
	}
	if got := claims.ExpiresAt.Sub(claims.IssuedAt.Time); got != accessTTL {
		t.Fatalf("有效期 = %s, want %s", got, accessTTL)
	}
}

func TestTokenManager_RejectsTokenFromAnotherSecret(t *testing.T) {
	manager, err := NewTokenManager(testSecret, testIssuer, time.Hour, 24*time.Hour)
	if err != nil {
		t.Fatalf("创建 TokenManager 失败: %v", err)
	}

	other, err := NewTokenManager(
		"abcdefghijklmnopqrstuvwxyz123456",
		testIssuer,
		time.Hour,
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("创建 TokenManager 失败: %v", err)
	}

	token, err := other.generateAccessToken(testUserID)
	if err != nil {
		t.Fatalf("生成 Access Token 失败: %v", err)
	}
	if _, err := manager.ParseAccessToken(token); err == nil {
		t.Fatal("其他密钥签发的令牌应当被拒绝")
	}
}
