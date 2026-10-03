package service

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	minHMACSecretLength = 32
	refreshTokenSize    = 32
)

// TokenManager 负责 Access Token 与 Refresh Token 的生成和校验
type TokenManager struct {
	secret          []byte
	issuer          string
	accessTokenTTL  time.Duration
	refreshTokenTTL time.Duration
}

func NewTokenManager(
	secret,
	issuer string,
	accessTokenTTL,
	refreshTokenTTL time.Duration,
) (*TokenManager, error) {
	if len(secret) < minHMACSecretLength {
		return nil, fmt.Errorf("secret 至少为 %d 字节", minHMACSecretLength)
	}

	if issuer == "" {
		return nil, errors.New("issuer 不能为空")
	}

	if accessTokenTTL <= 0 {
		return nil, errors.New("Access Token 有效期必须大于 0")
	}

	if refreshTokenTTL <= 0 {
		return nil, errors.New("Refresh Token 有效期必须大于 0")
	}

	return &TokenManager{
		secret:          []byte(secret),
		issuer:          issuer,
		accessTokenTTL:  accessTokenTTL,
		refreshTokenTTL: refreshTokenTTL,
	}, nil
}

// ParseAccessToken 解析并验证访问令牌,返回用户 ID（供中间件调用）
func (m *TokenManager) ParseAccessToken(tokenString string) (string, error) {
	claims := new(jwt.RegisteredClaims)

	_, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(_ *jwt.Token) (any, error) {
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil {
		return "", fmt.Errorf("验证访问令牌: %w", err)
	}

	if claims.Subject == "" {
		return "", errors.New("访问令牌缺少用户标识")
	}

	return claims.Subject, nil
}

// generateAccessToken 生成访问令牌
func (m *TokenManager) generateAccessToken(userID string) (string, error) {
	now := time.Now()

	claims := jwt.RegisteredClaims{
		Issuer:    m.issuer,
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(m.accessTokenTTL)),
	}

	tokenString, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).
		SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("生成访问令牌: %w", err)
	}

	return tokenString, nil
}

// generateRefreshToken 生成刷新令牌
func (m *TokenManager) generateRefreshToken() string {
	raw := make([]byte, refreshTokenSize)
	rand.Read(raw)

	return base64.RawURLEncoding.EncodeToString(raw)
}
