package database

import (
	"fmt"

	"github.com/jasper0507/bluebell/internal/config"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// 初始化 MySQL 数据库连接，并配置底层连接池
func Open(cfg *config.MySQLConfig) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(buildDSN(cfg)), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open MySQL: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database connection pool: %w", err)
	}

	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	return db, nil
}

// 根据config构建 MySQL DSN
func buildDSN(cfg *config.MySQLConfig) string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=true&loc=Local",
		cfg.Username,
		cfg.Password,
		cfg.Host,
		cfg.Port,
		cfg.Database,
	)
}

// 关闭 GORM 底层的数据库连接池
func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get database connection pool: %w", err)
	}

	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close MySQL: %w", err)
	}

	return nil
}
