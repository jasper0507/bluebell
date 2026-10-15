package service

import (
	"context"
	"testing"
	"time"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/store"
	"github.com/jasper0507/bluebell/internal/testenv"
	"github.com/jasper0507/bluebell/internal/worker"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// 库名和 Redis DB 与其他测试包错开，见 testenv 包注释。
const (
	testDatabase = "bluebell_test"
	testRedisDB  = 14
)

var (
	testDB    *gorm.DB
	testRedis *redis.Client
)

func reset(t *testing.T) {
	t.Helper()
	testDB = testenv.MySQL(t, testDatabase)
	testRedis = testenv.Redis(t, testRedisDB)
}

func startOutboxWorker(t *testing.T) (context.Context, context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	w := worker.NewOutboxWorker(repository.NewOutboxRepository(testDB), repository.NewPostRepository(testDB), store.NewPostStore(testRedis))
	done := make(chan error, 1)
	go func() {
		done <- w.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() { stopOutboxWorker(t, cancel, done) })
	return ctx, cancel, done
}

func stopOutboxWorker(t *testing.T, cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Outbox Worker 失败: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Error("Outbox Worker 退出超时")
	}
}

// syncOutbox 等待本次业务操作的通知全部确认，再停止 Worker。
func syncOutbox(t *testing.T) {
	t.Helper()

	ctx, cancel, done := startOutboxWorker(t)
	defer stopOutboxWorker(t, cancel, done)

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
		case err := <-done:
			t.Fatalf("Outbox Worker 提前退出，仍有 %d 条通知: %v", pending, err)
		case <-ctx.Done():
			t.Fatalf("Outbox 同步超时，仍有 %d 条通知", pending)
		case <-ticker.C:
		}
	}
}

func newPostTestServices(t *testing.T) (*repository.UserRepository, *UserService, *PostService) {
	t.Helper()

	userRepo := repository.NewUserRepository(testDB)
	posts := NewPostService(
		repository.NewPostRepository(testDB),
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
