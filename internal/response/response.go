package response

import "github.com/gin-gonic/gin"

// Response 统一响应结构
type Response struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// Success 返回成功响应
func Success(c *gin.Context, status int, data any) {
	c.JSON(status, Response{
		Code:    CodeOK,
		Message: CodeOK.Message(),
		Data:    data,
	})
}

// Error 返回错误响应
func Error(c *gin.Context, code Code) {
	// 中止后续处理并返回对应的 HTTP 状态码和 JSON 响应
	c.AbortWithStatusJSON(code.HTTPStatus(), Response{
		Code:    code,
		Message: code.Message(),
		Data:    nil,
	})
}
