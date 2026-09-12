package service

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const minHMACSecretLength = 32

type TokenService struct {
	secret         []byte
	issuer         string
	accessTokenTTL time.Duration
}

// NewTokenService 创建Token服务
func NewTokenService(secret string, issuer string, accessTokenTTL time.Duration) (*TokenService, error) {
	if len(secret) < minHMACSecretLength {
		return nil, fmt.Errorf("secret 至少为 %d 字节", minHMACSecretLength)
	}

	if issuer == "" {
		return nil, errors.New("issuer不能为空")
	}

	if accessTokenTTL <= 0 {
		return nil, errors.New("JWT access token 有效期必须大于 0")
	}

	return &TokenService{
		secret:         []byte(secret),
		issuer:         issuer,
		accessTokenTTL: accessTokenTTL,
	}, nil
}

// GenerateAccessToken 生成访问令牌
func (s *TokenService) GenerateAccessToken(userID string) (string, error) {
	now := time.Now()

	// 创建JWT声明
	claims := jwt.RegisteredClaims{
		Issuer:    s.issuer,
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTokenTTL)),
	}

	// 创建JWT令牌
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)

	// 签名并获取令牌字符串
	tokenString, err := token.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("生成访问令牌：%w", err)
	}

	return tokenString, nil
}

// ParseAccessToken 解析并验证访问令牌
func (s *TokenService) ParseAccessToken(tokenString string) (string, error) {
	claims := new(jwt.RegisteredClaims)

	_, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(_ *jwt.Token) (any, error) {
			return s.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), // 仅允许 HS256 签名算法
		jwt.WithIssuer(s.issuer),     // 校验签发者
		jwt.WithExpirationRequired(), // 要求 exp 必填并校验过期时间
		jwt.WithIssuedAt(),           // 校验 iat 签发时间
	)
	if err != nil {
		return "", fmt.Errorf("验证访问令牌: %w", err)
	}

	if claims.Subject == "" {
		return "", errors.New("访问令牌缺少用户标识")
	}

	return claims.Subject, nil
}
