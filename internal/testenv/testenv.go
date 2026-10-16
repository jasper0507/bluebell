// Package testenv 为集成测试提供已清空的 MySQL 库和 Redis DB。
//
// go test 会并行运行多个包，且每个测试开始前都会清库，
// 因此每个测试包必须使用独立的 MySQL 库名和 Redis DB，且包内固定不变。
// 数据只在测试开始前清理，测试结束后保留，便于排查失败用例。
//
// 当前分配：
//
//	业务（configs/config.yaml）：MySQL ripple，Redis DB 0
//	压测（tests/load/config.yaml）：MySQL ripple_k6_verify，Redis DB 13
//	internal/app：MySQL ripple_test_app，Redis DB 12
//	internal/service：MySQL ripple_test，Redis DB 14
//	internal/store：Redis DB 15
package testenv

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jasper0507/ripple/internal/cache"
	"github.com/jasper0507/ripple/internal/config"
	"github.com/jasper0507/ripple/internal/database"
	"github.com/jasper0507/ripple/internal/model"
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

const testDatabasePrefix = "ripple_test"

// reservedRedisDBs 是业务和压测使用的 Redis DB，测试清库会破坏其中的数据
var reservedRedisDBs = map[int]string{
	0:  "业务",
	13: "压测",
}

// MySQL 返回已迁移并清空所有表（包括软删除记录）的测试库。
func MySQL(t *testing.T, name string) *gorm.DB {
	t.Helper()

	if !strings.HasPrefix(name, testDatabasePrefix) {
		t.Fatalf("测试库名 %q 必须以 %s 开头，避免清空业务或压测库", name, testDatabasePrefix)
	}

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

	if owner, ok := reservedRedisDBs[db]; ok {
		t.Fatalf("Redis DB %d 保留给%s，测试不能使用", db, owner)
	}

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
	if err := db.AutoMigrate(model.All()...); err != nil {
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
