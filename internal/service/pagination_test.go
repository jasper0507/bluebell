package service

import (
	"errors"
	"math"
	"testing"
)

func TestPaginationOffset(t *testing.T) {
	for _, tt := range []struct {
		name     string
		page     int
		pageSize int
		want     int
		wantErr  bool
	}{
		{name: "第一页", page: 1, pageSize: 10, want: 0},
		{name: "第三页", page: 3, pageSize: 10, want: 20},
		{name: "页尾接近最大整数", page: math.MaxInt / 7, pageSize: 7, want: math.MaxInt/7*7 - 7},
		{name: "页码为零", pageSize: 10, wantErr: true},
		{name: "负数页码", page: -1, pageSize: 10, wantErr: true},
		{name: "每页数量为零", page: 1, wantErr: true},
		{name: "负数每页数量", page: 1, pageSize: -1, wantErr: true},
		{name: "页尾超出整数范围", page: math.MaxInt/10 + 1, pageSize: 10, wantErr: true},
		{name: "最大整数页码", page: math.MaxInt, pageSize: 10, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := paginationOffset(tt.page, tt.pageSize)
			if tt.wantErr {
				if !errors.Is(err, ErrInvalidPagination) {
					t.Fatalf("err = %v, want ErrInvalidPagination", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("offset = %d, err = %v, want %d", got, err, tt.want)
			}
		})
	}
}
