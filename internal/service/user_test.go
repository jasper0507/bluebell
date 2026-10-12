package service

import (
	"errors"
	"testing"
)

func TestUser_RegisterRejectsDuplicateUsername(t *testing.T) {
	reset(t)
	ctx := t.Context()
	_, users, _ := newPostTestServices(t)

	if err := users.Register(ctx, "alice01", "password1"); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	if err := users.Register(ctx, "alice01", "password2"); !errors.Is(err, ErrUsernameExists) {
		t.Fatalf("重复注册 err = %v, want ErrUsernameExists", err)
	}
}
