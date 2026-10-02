package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"github.com/jasper0507/bluebell/internal/repository"
)

type AuthService struct {
	userRepo           *repository.UserRepository
	accessTokenService *AccessTokenService
}

func NewAuthService(
	userRepo *repository.UserRepository,
	accessTokenService *AccessTokenService,
) *AuthService {
	return &AuthService{
		userRepo:           userRepo,
		accessTokenService: accessTokenService,
	}
}

var ErrInvalidCredentials = errors.New("用户名或密码错误")

const refreshTokenSize = 32

func generateRefreshToken() string {
	raw := make([]byte, refreshTokenSize)
	rand.Read(raw)

	return base64.RawURLEncoding.EncodeToString(raw)
}

// Login 用户登录
func (s *AuthService) Login(ctx context.Context, username, password string) (string, error) {
	// 1. 根据用户名查找用户
	user, err := s.userRepo.FindByUsername(ctx, username)

	// 用户不存在
	if errors.Is(err, repository.ErrUserNotFound) {
		return "", ErrInvalidCredentials
	}

	// 数据库查询失败
	if err != nil {
		return "", err
	}

	// 2. 检验密码
	if err := verifyPassword(user.PasswordHash, password); err != nil {
		return "", ErrInvalidCredentials
	}

	// 3. 生成访问令牌
	accessToken, err := s.accessTokenService.GenerateAccessToken(user.UserID)

	if err != nil {
		return "", err
	}

	return accessToken, nil
}
