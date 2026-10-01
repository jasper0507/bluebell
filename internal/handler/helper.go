package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

// parseUintParam 解析路径参数中的正整数（uint），0 视为无效
func parseUintParam(c *gin.Context, name string) (uint, bool) {
	value, err := strconv.ParseUint(c.Param(name), 10, strconv.IntSize)
	if err != nil || value == 0 {
		return 0, false
	}

	return uint(value), true
}
