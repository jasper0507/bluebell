package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/store"
)

type AuthService struct {
	userRepo           *repository.UserRepository
	accessTokenService *AccessTokenService
	refreshTokenStore  *store.RefreshTokenStore
	refreshTokenTTL    time.Duration
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

type AuthTokens struct {
	AccessToken     string
	RefreshToken    string
	RefreshTokenTTL time.Duration
}

var ErrInvalidCredentials = errors.New("用户名或密码错误")

const refreshTokenSize = 32

func generateRefreshToken() string {
	raw := make([]byte, refreshTokenSize)
	rand.Read(raw)

	return base64.RawURLEncoding.EncodeToString(raw)
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

	// 3. 生成 Access Token
	accessToken, err := s.accessTokenService.GenerateAccessToken(user.UserID)
	if err != nil {
		return nil, err
	}

	// 4. 生成 Refresh Token
	refreshToken := generateRefreshToken()

	// 5. 保存 Refresh Token
	if err := s.refreshTokenStore.Save(
		ctx,
		refreshToken,
		user.UserID,
		s.refreshTokenTTL,
	); err != nil {
		return nil, err
	}

	return &AuthTokens{
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		RefreshTokenTTL: s.refreshTokenTTL,
	}, nil
}
