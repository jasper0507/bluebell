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

// Refresh 刷新认证令牌
func (s *AuthService) Refresh(
	ctx context.Context,
	refreshToken string,
) (*AuthTokens, error) {
	// 1. 生成新的 Refresh Token
	newRefreshToken := s.tokenManager.generateRefreshToken()

	// 2. 原子轮换 Refresh Token，并获取userID和剩余有效期
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

	// 3. 为当前用户生成新的 Access Token
	accessToken, err := s.tokenManager.generateAccessToken(userID)
	if err != nil {
		return nil, err
	}

	// 4. 返回新的令牌对，新 Refresh Token 继承原会话剩余有效期
	return &AuthTokens{
		AccessToken:     accessToken,
		RefreshToken:    newRefreshToken,
		RefreshTokenTTL: ttl,
	}, nil
}

// issueTokens 为用户签发一对令牌,并保存 Refresh Token
func (s *AuthService) issueTokens(
	ctx context.Context,
	userID string,
) (*AuthTokens, error) {
	// 1. 生成 Access Token
	accessToken, err := s.tokenManager.generateAccessToken(userID)
	if err != nil {
		return nil, err
	}

	// 2. 生成 Refresh Token
	refreshToken := s.tokenManager.generateRefreshToken()

	// 3. 保存 Refresh Token
	if err := s.refreshTokenStore.Save(
		ctx,
		refreshToken,
		userID,
		s.tokenManager.accessTokenTTL,
	); err != nil {
		return nil, err
	}

	return &AuthTokens{
		AccessToken:     accessToken,
		RefreshToken:    refreshToken,
		RefreshTokenTTL: s.tokenManager.refreshTokenTTL,
	}, nil
}
