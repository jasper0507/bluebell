package logger

import (
	"log/slog"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// NewGORM 创建写入 slog 的 GORM 日志记录器
func NewGORM(l *slog.Logger) gormlogger.Interface {
	return gormlogger.NewSlogLogger(l, gormlogger.Config{
		SlowThreshold:             200 * time.Millisecond,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		LogLevel:                  gormlogger.Warn,
	})
}
