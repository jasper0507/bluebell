package service

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/jasper0507/bluebell/internal/cache"
	"github.com/jasper0507/bluebell/internal/config"
	"github.com/jasper0507/bluebell/internal/database"
	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// service 与 store 的测试会被 go test 并行执行，因此使用不同的 Redis DB。
const (
	testMySQLAddr     = "127.0.0.1:3306"
	testRedisAddr     = "127.0.0.1:6379"
	testMySQLUser     = "root"
	testMySQLPassword = "root_password"
	testDatabase      = "bluebell_test"
	testRedisDB       = 14

	authSecret = "12345678901234567890123456789012"
	authIssuer = "bluebell-itest"
)

var (
	testDB    *gorm.DB
	testRedis *redis.Client

	openOnce sync.Once
	openErr  error
)

func reset(t *testing.T) {
	t.Helper()

	openOnce.Do(func() {
		testDB, testRedis, openErr = openTestDeps()
	})
	if openErr != nil {
		t.Fatalf("集成测试需要先执行 make up 启动 MySQL 和 Redis: %v", openErr)
	}

	if err := testRedis.FlushDB(t.Context()).Err(); err != nil {
		t.Fatalf("清空 Redis 测试库失败: %v", err)
	}

	statements := []string{
		"SET FOREIGN_KEY_CHECKS = 0",
		"TRUNCATE TABLE comments",
		"TRUNCATE TABLE posts",
		"TRUNCATE TABLE users",
		"TRUNCATE TABLE communities",
		"SET FOREIGN_KEY_CHECKS = 1",
	}
	for _, statement := range statements {
		if err := testDB.Exec(statement).Error; err != nil {
			t.Fatalf("重置数据库失败: %s: %v", statement, err)
		}
	}
}

func openTestDeps() (*gorm.DB, *redis.Client, error) {
	if err := dial(testMySQLAddr); err != nil {
		return nil, nil, fmt.Errorf("MySQL 未就绪: %w", err)
	}
	if err := dial(testRedisAddr); err != nil {
		return nil, nil, fmt.Errorf("Redis 未就绪: %w", err)
	}

	adminCfg := testMySQLConfig("mysql")
	admin, err := database.Open(&adminCfg)
	if err != nil {
		return nil, nil, err
	}

	createDB := "CREATE DATABASE IF NOT EXISTS " + testDatabase +
		" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
	if err := admin.Exec(createDB).Error; err != nil {
		_ = database.Close(admin)
		return nil, nil, fmt.Errorf("创建测试库: %w", err)
	}
	if err := database.Close(admin); err != nil {
		return nil, nil, err
	}

	cfg := testMySQLConfig(testDatabase)
	mysqlDB, err := database.Open(&cfg)
	if err != nil {
		return nil, nil, err
	}
	if err := mysqlDB.AutoMigrate(
		&model.User{},
		&model.Community{},
		&model.Post{},
		&model.Comment{},
	); err != nil {
		_ = database.Close(mysqlDB)
		return nil, nil, fmt.Errorf("迁移测试库: %w", err)
	}

	redisClient, err := cache.Open(context.Background(), &config.RedisConfig{
		Addr: testRedisAddr,
		DB:   testRedisDB,
	})
	if err != nil {
		_ = database.Close(mysqlDB)
		return nil, nil, err
	}

	return mysqlDB, redisClient, nil
}

func testMySQLConfig(databaseName string) config.MySQLConfig {
	return config.MySQLConfig{
		Host:            "127.0.0.1",
		Port:            3306,
		Username:        testMySQLUser,
		Password:        testMySQLPassword,
		Database:        databaseName,
		MaxOpenConns:    10,
		MaxIdleConns:    10,
		ConnMaxLifetime: time.Minute,
	}
}

func dial(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

func newAuth(
	t *testing.T,
	refreshTTL time.Duration,
) (*repository.UserRepository, *UserService, *AuthService, *TokenManager) {
	t.Helper()

	userRepo := repository.NewUserRepository(testDB)
	manager, err := NewTokenManager(authSecret, authIssuer, time.Hour, refreshTTL)
	if err != nil {
		t.Fatalf("创建 TokenManager 失败: %v", err)
	}

	authService := NewAuthService(
		userRepo,
		store.NewRefreshTokenStore(testRedis),
		manager,
	)

	return userRepo, NewUserService(userRepo), authService, manager
}

func newPostService(t *testing.T) (*repository.UserRepository, *UserService, *PostService) {
	t.Helper()

	userRepo := repository.NewUserRepository(testDB)
	posts := NewPostService(
		repository.NewPostRepository(testDB),
		userRepo,
		repository.NewCommunityRepository(testDB),
		store.NewPostStore(testRedis),
	)

	return userRepo, NewUserService(userRepo), posts
}

func seedCommunity(t *testing.T, name string) uint {
	t.Helper()

	community := &model.Community{
		Name:         name,
		Introduction: name,
	}
	if err := testDB.Create(community).Error; err != nil {
		t.Fatalf("创建社区失败: %v", err)
	}

	return community.ID
}

func registerUser(
	t *testing.T,
	users *UserService,
	userRepo *repository.UserRepository,
	username, password string,
) string {
	t.Helper()

	ctx := t.Context()
	if err := users.Register(ctx, username, password); err != nil {
		t.Fatalf("注册 %s 失败: %v", username, err)
	}

	user, err := userRepo.FindByUsername(ctx, username)
	if err != nil {
		t.Fatalf("查询用户 %s 失败: %v", username, err)
	}

	return user.UserID
}
