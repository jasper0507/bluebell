package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/response"
)

const ContextUserIDKey = "user_id"

type AccessTokenVerifier interface {
	ParseAccessToken(string) (string, error)
}

// JWTAuth 中间件，用于验证 JWT 令牌
func JWTAuth(verifier AccessTokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 获取 Authorization 请求头
		authHeader := c.GetHeader("Authorization")

		// 2. 校验 Bearer Token 格式
		parts := strings.Fields(authHeader)

		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			response.Error(c, response.CodeUnauthorized)
			return
		}

		// 3. 解析 Access Token
		userID, err := verifier.ParseAccessToken(parts[1])
		if err != nil {
			response.Error(c, response.CodeUnauthorized)
			return
		}

		// 4. 保存用户ID
		c.Set(ContextUserIDKey, userID)
	}
}
