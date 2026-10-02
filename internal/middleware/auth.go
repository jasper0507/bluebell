package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jasper0507/bluebell/internal/response"
	"github.com/jasper0507/bluebell/internal/service"
)

const ContextUserIDKey = "user_id"

// JWTAuth 中间件，用于验证 JWT 令牌
func JWTAuth(accessTokenService *service.AccessTokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 获取 Authorization 请求头
		authHeader := c.GetHeader("Authorization")

		// 2. 校验 Bearer Token 格式
		parts := strings.Fields(authHeader)

		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			response.Error(c, response.CodeUnauthorized)
			return
		}

		// 3. 解析 Token
		userID, err := accessTokenService.ParseAccessToken(parts[1])

		if err != nil {
			response.Error(c, response.CodeUnauthorized)
			return
		}

		// 4. 将 userID 存储到上下文
		c.Set(ContextUserIDKey, userID)
	}
}
