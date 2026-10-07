package service

import (
	"context"
	"errors"
	"time"

	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/store"
)

type AuthService struct {
	userRepo          *repository.UserRepository
	refreshTokenStore *store.RefreshTokenStore

	tokenManager *TokenManager
}

func NewAuthService(
	userRepo *repository.UserRepository,
	refreshTokenStore *store.RefreshTokenStore,
	tokenManager *TokenManager,
) *AuthService {
	return &AuthService{
		userRepo:          userRepo,
		refreshTokenStore: refreshTokenStore,
		tokenManager:      tokenManager,
	}
}

// AuthTokens 用于存储认证令牌
type AuthTokens struct {
	AccessToken     string
	RefreshToken    string
	RefreshTokenTTL time.Duration
}

var (
	ErrInvalidCredentials  = errors.New("用户名或密码错误")
	ErrInvalidRefreshToken = errors.New("刷新令牌无效或已过期")
)

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
	return s.issueTokens(ctx, user.UserID)
}

// Logout 退出登录
func (s *AuthService) Logout(
	ctx context.Context,
	refreshToken string,
) error {
	return s.refreshTokenStore.Delete(ctx, refreshToken)
}

// Refresh 刷新认证令牌
func (s *AuthService) Refresh(
	ctx context.Context,
	refreshToken string,
) (*AuthTokens, error) {
	// 1. 生成新的刷新令牌
	newRefreshToken := s.tokenManager.generateRefreshToken()

	// 2. 原子轮换刷新令牌，并获取用户 ID 和剩余有效期
	userID, ttl, err := s.refreshTokenStore.Rotate(
		ctx,
		refreshToken,
		newRefreshToken,
	)
	if errors.Is(err, store.ErrRefreshTokenNotFound) {
		return nil, ErrInvalidRefreshToken
	}
	if err != nil {
		return nil, err
	}

	// 3. 为当前用户生成新的访问令牌
	accessToken, err := s.tokenManager.generateAccessToken(userID)
	if err != nil {
		return nil, err
	}

	// 4. 返回新的令牌对，新刷新令牌继承原会话剩余有效期
	return &AuthTokens{
		AccessToken:     accessToken,
		RefreshToken:    newRefreshToken,
		RefreshTokenTTL: ttl,
	}, nil
}

// issueTokens 为用户签发一对令牌，并保存刷新令牌
func (s *AuthService) issueTokens(
	ctx context.Context,
	userID string,
) (*AuthTokens, error) {
	// 1. 生成访问令牌
	accessToken, err := s.tokenManager.generateAccessToken(userID)
	if err != nil {
		return nil, err
	}

	// 2. 生成刷新令牌
	refreshToken := s.tokenManager.generateRefreshToken()

	// 3. 保存刷新令牌
	if err := s.refreshTokenStore.Save(
		ctx,
		refreshToken,
		userID,
		s.tokenManager.refreshTokenTTL,
	); err != nil {
		return nil, err
	}

	return &AuthTokens{
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		RefreshTokenTTL: s.tokenManager.refreshTokenTTL,
	}, nil
}
