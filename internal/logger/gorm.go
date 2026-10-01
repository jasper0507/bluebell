package logger

import (
	"log"
	"os"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// NewGORM 创建 GORM 日志记录器
func NewGORM() gormlogger.Interface {
	return gormlogger.New(
		log.New(os.Stdout, "\r\n", log.LstdFlags),
		gormlogger.Config{
			SlowThreshold:             200 * time.Millisecond,
			Colorful:                  true,
			IgnoreRecordNotFoundError: true,
			ParameterizedQueries:      false,
			LogLevel:                  gormlogger.Warn,
		},
	)
}
