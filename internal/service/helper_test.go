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
	"github.com/jasper0507/bluebell/internal/worker"
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

	// 测试使用创建后返回的 ID，无需重置自增序列。
	// 在同一事务中物理清库（包含软删除记录），避免逐表 TRUNCATE 的 DDL 开销。
	err := testDB.WithContext(t.Context()).Transaction(func(tx *gorm.DB) error {
		for _, statement := range []string{
			"DELETE FROM outbox_events",
			"DELETE FROM post_votes",
			"DELETE FROM comments",
			"DELETE FROM posts",
			"DELETE FROM users",
			"DELETE FROM communities",
		} {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("%s: %w", statement, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("重置数据库失败: %v", err)
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
		&model.PostVote{},
		&model.OutboxEvent{},
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

// syncOutbox 等待本次业务操作的通知全部确认，再停止 Worker。
func syncOutbox(t *testing.T) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	w := worker.NewOutboxWorker(
		repository.NewOutboxRepository(testDB),
		repository.NewPostRepository(testDB),
		store.NewPostStore(testRedis),
	)
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Outbox Worker 失败: %v", err)
		}
	}()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var pending int64
		if err := testDB.WithContext(ctx).Model(&model.OutboxEvent{}).Count(&pending).Error; err != nil {
			t.Fatalf("查询待同步通知失败: %v", err)
		}
		if pending == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Outbox 同步超时，仍有 %d 条通知", pending)
		case <-ticker.C:
		}
	}
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

func newAuthTestServices(
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

func newPostTestServices(t *testing.T) (*repository.UserRepository, *UserService, *PostService) {
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
