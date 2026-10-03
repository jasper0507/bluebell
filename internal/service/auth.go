package service

import (
	"context"
	"errors"
	"time"

	"github.com/jasper0507/bluebell/internal/repository"
)

var ErrInvalidCredentials = errors.New("用户名或密码错误")

type AuthService struct {
	userRepo     *repository.UserRepository
	tokenManager *TokenManager
}

func NewAuthService(
	userRepo *repository.UserRepository,
	tokenManager *TokenManager,
) *AuthService {
	return &AuthService{
		userRepo:     userRepo,
		tokenManager: tokenManager,
	}
}

// AuthTokens 用于存储认证令牌
type AuthTokens struct {
	AccessToken     string
	RefreshToken    string
	RefreshTokenTTL time.Duration
}

// Login 用户登录
func (s *AuthService) Login(
	ctx context.Context,
	username,
	password string,
) (*AuthTokens, error) {
	// 1. 根据用户名查找用户
	user, err := s.userRepo.FindByUsername(ctx, username)
	if errors.Is(err, repository.ErrUserNotFound) {
		return nil, ErrInvalidCredentials
	}
	if err != nil {
		return nil, err
	}

	// 2. 校验密码
	if err := verifyPassword(user.PasswordHash, password); err != nil {
		return nil, ErrInvalidCredentials
	}

	// 3. 签发令牌
	return s.tokenManager.IssueTokens(ctx, user.UserID)
}
