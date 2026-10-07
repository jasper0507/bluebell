package service

import (
	"errors"
	"math"
)

var ErrInvalidPagination = errors.New("分页参数无效")

// paginationOffset 确保整页范围不会发生整数溢出
func paginationOffset(page, pageSize int) (int, error) {
	if page < 1 || pageSize < 1 {
		return 0, ErrInvalidPagination
	}

	// 用除法检查边界，避免检查表达式本身溢出
	if page > math.MaxInt/pageSize {
		return 0, ErrInvalidPagination
	}

	return (page - 1) * pageSize, nil
}
