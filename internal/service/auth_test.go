package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"
)

func TestAuth_LoginRefreshLogout(t *testing.T) {
	reset(t)

	ctx := t.Context()
	const (
		username   = "alice01"
		password   = "password1"
		refreshTTL = time.Hour
	)

	userRepo, users, authService, manager := newAuth(t, refreshTTL)
	if err := users.Register(ctx, username, password); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	for _, tt := range []struct {
		name, user, pass string
	}{
		{name: "错误密码", user: username, pass: "badpass1"},
		{name: "用户不存在", user: "nobody01", pass: password},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := authService.Login(ctx, tt.user, tt.pass); !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("err = %v", err)
			}
		})
	}

	tokens, err := authService.Login(ctx, username, password)
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}

	user, err := userRepo.FindByUsername(ctx, username)
	if err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	userID, err := manager.ParseAccessToken(tokens.AccessToken)
	if err != nil {
		t.Fatalf("解析 access token 失败: %v", err)
	}
	if userID != user.UserID {
		t.Fatalf("access token 用户 = %q, want %q", userID, user.UserID)
	}

	// 缩短剩余有效期，确认刷新沿用这段时间，而不是重新签发 refreshTTL。
	key := refreshTokenKey(tokens.RefreshToken)
	if err := testRedis.PExpire(ctx, key, 5*time.Second).Err(); err != nil {
		t.Fatalf("缩短 TTL 失败: %v", err)
	}

	next, err := authService.Refresh(ctx, tokens.RefreshToken)
	if err != nil {
		t.Fatalf("刷新失败: %v", err)
	}
	if next.RefreshToken == "" || next.RefreshToken == tokens.RefreshToken {
		t.Fatal("刷新后应当换发新的 refresh token")
	}
	if next.RefreshTokenTTL <= 3*time.Second || next.RefreshTokenTTL > 5*time.Second {
		t.Fatalf("刷新后的 ttl = %s, 应当继承约 5s 的剩余有效期", next.RefreshTokenTTL)
	}
	if got, err := manager.ParseAccessToken(next.AccessToken); err != nil || got != user.UserID {
		t.Fatalf("新 access token 用户 = %q, err = %v", got, err)
	}
	if _, err := authService.Refresh(ctx, tokens.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("旧 refresh token 再次使用 err = %v", err)
	}

	if err := authService.Logout(ctx, next.RefreshToken); err != nil {
		t.Fatalf("退出失败: %v", err)
	}
	if _, err := authService.Refresh(ctx, next.RefreshToken); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("退出后刷新 err = %v", err)
	}
}

func refreshTokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return "bluebell:auth:refresh:" + hex.EncodeToString(sum[:])
}
