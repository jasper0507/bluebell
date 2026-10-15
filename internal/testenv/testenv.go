// Package testenv 为集成测试提供已清空的 MySQL 库和 Redis DB。
//
// go test 会并行运行多个包，且每个测试开始前都会清库，
// 因此每个测试包必须使用独立的 MySQL 库名和 Redis DB，且包内固定不变。
package testenv

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jasper0507/bluebell/internal/cache"
	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/database"
	"github.com/jasper0507/bluebell/internal/model"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

var (
	mysqlOnce sync.Once
	mysqlDB   *gorm.DB
	mysqlErr  error

	redisOnce   sync.Once
	redisClient *redis.Client
	redisErr    error
)

// MySQL 返回已迁移并清空所有表（包括软删除记录）的测试库。
func MySQL(t *testing.T, name string) *gorm.DB {
	t.Helper()

	mysqlOnce.Do(func() { mysqlDB, mysqlErr = openMySQL(name) })
	if mysqlErr != nil {
		t.Fatalf("集成测试需要先执行 make up 启动 MySQL: %v", mysqlErr)
	}

	// 测试使用创建后返回的 ID，无需重置自增序列；DELETE 比逐表 TRUNCATE 快。
	err := mysqlDB.WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
		for _, table := range []string{
			"outbox_events", "post_votes", "comments", "posts", "users", "communities",
		} {
			if err := tx.Exec("DELETE FROM " + table).Error; err != nil {
				return fmt.Errorf("清空 %s: %w", table, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("重置 MySQL 测试库失败: %v", err)
	}

	return mysqlDB
}

// Redis 返回已清空的测试 Redis DB。
func Redis(t *testing.T, db int) *redis.Client {
	t.Helper()

	redisOnce.Do(func() {
		redisClient, redisErr = cache.Open(context.Background(), &config.RedisConfig{
			Addr: "127.0.0.1:6379",
			DB:   db,
		})
	})
	if redisErr != nil {
		t.Fatalf("集成测试需要先执行 make up 启动 Redis: %v", redisErr)
	}

	if err := redisClient.FlushDB(t.Context()).Err(); err != nil {
		t.Fatalf("清空 Redis 测试库失败: %v", err)
	}

	return redisClient
}

func openMySQL(name string) (*gorm.DB, error) {
	admin, err := database.Open(mysqlConfig("mysql"))
	if err != nil {
		return nil, err
	}
	createDB := "CREATE DATABASE IF NOT EXISTS " + name +
		" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	err = admin.Exec(createDB).Error
	if closeErr := database.Close(admin); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("创建测试库 %s: %w", name, err)
	}

	db, err := database.Open(mysqlConfig(name))
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(
		&model.User{},
		&model.Community{},
		&model.Post{},
		&model.Comment{},
		&model.PostVote{},
		&model.OutboxEvent{},
	); err != nil {
		_ = database.Close(db)
		return nil, fmt.Errorf("迁移测试库 %s: %w", name, err)
	}

	return db, nil
}

func mysqlConfig(name string) *config.MySQLConfig {
	return &config.MySQLConfig{
		Host:            "127.0.0.1",
		Port:            3306,
		Username:        "root",
		Password:        "root_password",
		Database:        name,
		MaxOpenConns:    10,
		MaxIdleConns:    10,
		ConnMaxLifetime: time.Minute,
	}
}
