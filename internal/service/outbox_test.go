package service

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func TestOutbox_BoundedConcurrencyAndBatchAck(t *testing.T) {
	reset(t)
	posts := seedOutboxPosts(t, 9)
	// 同帖重复事件应合并为一个任务，不占用额外并发槽位。
	extra := model.OutboxEvent{EventType: model.EventPostIndexSync, AggregateID: posts[0].ID}
	if err := testDB.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}

	started := make(chan uint, len(posts)+1)
	release := make(chan struct{})
	onOutboxPostQuery(t, func(tx *gorm.DB, post *model.Post) {
		started <- post.ID
		select {
		case <-release:
		case <-tx.Statement.Context.Done():
			tx.AddError(tx.Statement.Context.Err())
		}
	})
	ctx, cancel, done := startOutboxWorker(t)

	seen := make(map[uint]int)
	waitStarted := func() {
		t.Helper()
		select {
		case id := <-started:
			seen[id]++
		case <-ctx.Done():
			t.Fatal("等待并发任务启动超时")
		}
	}
	// 串行实现无法在任何任务被释放前启动四个帖子任务。
	for range 4 {
		waitStarted()
	}
	select {
	case id := <-started:
		t.Fatalf("四个任务阻塞时仍启动了帖子 %d，超过并发上限", id)
	case <-time.After(100 * time.Millisecond):
	}

	// 每次只放行一个任务，确认槽位复用且成功事件尚未提前删除。
	for range len(posts) - 4 {
		select {
		case release <- struct{}{}:
		case <-ctx.Done():
			t.Fatal("放行任务超时")
		}
		waitStarted()
		if events := findOutboxEvents(t); len(events) != len(posts)+1 {
			t.Fatalf("整批尚未完成就确认事件，剩余 %d 条", len(events))
		}
	}
	close(release)
	waitOutbox(t, ctx, func() bool { return len(findOutboxEvents(t)) == 0 })
	stopOutboxWorker(t, cancel, done)

	for _, post := range posts {
		if seen[post.ID] != 1 {
			t.Errorf("帖子 %d 执行 %d 次，want 1", post.ID, seen[post.ID])
		}
	}
	postStore := store.NewPostStore(testRedis)
	for _, scope := range []*uint{nil, &posts[0].CommunityID} {
		for _, order := range []string{"time", "hot"} {
			ids, total, err := postStore.FindPostIDs(t.Context(), scope, order, 0, len(posts))
			if err != nil || total != int64(len(posts)) || len(ids) != len(posts) {
				t.Errorf("%s 排行 = %v, total = %d, err = %v", order, ids, total, err)
			}
		}
	}
}

func TestOutbox_CancelWaitsForTasksAndPreservesEvents(t *testing.T) {
	reset(t)
	seedOutboxPosts(t, 9)
	want := findOutboxEvents(t)

	started := make(chan struct{}, 9)
	cancelled := make(chan struct{}, 9)
	completeOne := make(chan struct{})
	release := make(chan struct{})
	// 失败路径也要释放回调，避免测试清理被阻塞。
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	restore := onOutboxPostQuery(t, func(tx *gorm.DB, _ *model.Post) {
		started <- struct{}{}
		select {
		case <-completeOne:
			return
		case <-tx.Statement.Context.Done():
		}
		cancelled <- struct{}{}
		<-release
		tx.AddError(tx.Statement.Context.Err())
	})
	ctx, cancel, done := startOutboxWorker(t)
	for range 4 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("等待任务启动超时")
		}
	}
	// 先完成一个任务并等候槽位复用，取消时成功任务也不能提前确认。
	select {
	case completeOne <- struct{}{}:
	case <-ctx.Done():
		t.Fatal("放行任务超时")
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("等待后续任务启动超时")
	}
	cancel()
	for range 4 {
		select {
		case <-cancelled:
		case <-time.After(5 * time.Second):
			t.Fatal("任务未收到取消信号")
		}
	}
	select {
	case <-done:
		t.Fatal("Worker 在已启动任务结束前返回")
	case <-time.After(100 * time.Millisecond):
	}
	if got := findOutboxEvents(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("取消时事件被删除或重试状态被修改：got %+v, want %+v", got, want)
	}
	unblock()
	stopOutboxWorker(t, cancel, done)
	if got := findOutboxEvents(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("退出后事件发生变化：got %+v, want %+v", got, want)
	}

	// 重启后未确认事件应能正常同步。
	restore()
	syncOutbox(t)
}

func TestOutbox_MixedResultsRetryOnlyFailedPostEvents(t *testing.T) {
	reset(t)
	ctx := t.Context()
	posts := seedOutboxPosts(t, 6)
	postRepo := repository.NewPostRepository(testDB)
	for i, post := range posts {
		for _, direction := range []int8{1, -1, 1} {
			if err := postRepo.Vote(ctx, post.ID, "voter-a", direction); err != nil {
				t.Fatal(err)
			}
		}
		if err := postRepo.Vote(ctx, post.ID, "voter-b", int8(i%3-1)); err != nil {
			t.Fatal(err)
		}
	}
	// 一帖投影不完整，另一帖已被物理删除，分别覆盖 Redis/MySQL 失败。
	if err := testRedis.ZAdd(ctx, "bluebell:post:vote_scores", redis.Z{
		Member: strconv.FormatUint(uint64(posts[1].ID), 10), Score: 0,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Unscoped().Delete(&posts[3]).Error; err != nil {
		t.Fatal(err)
	}
	// 已软删除的帖子应清理投影并确认同帖所有事件。
	postStore := store.NewPostStore(testRedis)
	if err := postStore.InitPost(ctx, posts[5].ID, posts[5].CommunityID, posts[5].CreatedAt); err != nil {
		t.Fatal(err)
	}
	if err := postStore.ApplyVote(ctx, posts[5].ID, posts[5].CommunityID, "voter-a", 1, posts[5].CreatedAt); err != nil {
		t.Fatal(err)
	}
	if err := postRepo.Delete(ctx, posts[5].ID); err != nil {
		t.Fatal(err)
	}

	wantFailed := make(map[uint64]model.OutboxEvent)
	for _, event := range findOutboxEvents(t) {
		if event.AggregateID == posts[1].ID || event.AggregateID == posts[3].ID {
			wantFailed[event.ID] = event
		}
	}
	workerCtx, cancel, done := startOutboxWorker(t)
	waitOutbox(t, workerCtx, func() bool {
		return len(findOutboxEvents(t)) == len(wantFailed)
	})
	stopOutboxWorker(t, cancel, done)
	for _, event := range findOutboxEvents(t) {
		before, ok := wantFailed[event.ID]
		if !ok {
			t.Errorf("成功事件 %d 未确认", event.ID)
			continue
		}
		if event.RetryCount != 1 || !event.NextRetryAt.After(before.NextRetryAt) || event.LastError == "" {
			t.Errorf("失败事件重试信息不正确：%+v", event)
		}
		if event.AggregateID == posts[1].ID && !strings.Contains(event.LastError, "post projection incomplete") {
			t.Errorf("Redis 错误归属不正确：%+v", event)
		}
		if event.AggregateID == posts[3].ID && event.LastError != repository.ErrPostNotFound.Error() {
			t.Errorf("MySQL 错误归属不正确：%+v", event)
		}
	}
	for _, i := range []int{0, 2, 4} {
		postID := strconv.FormatUint(uint64(posts[i].ID), 10)
		score, err := testRedis.ZScore(ctx, "bluebell:post:vote_scores", postID).Result()
		if want := float64(i % 3); err != nil || score != want {
			t.Errorf("帖子 %s 净分 = %g, err = %v, want %g", postID, score, err, want)
		}
		votes, err := testRedis.HGetAll(ctx, "bluebell:post:votes:"+postID).Result()
		wantVotes := map[string]string{"voter-a": "1"}
		if direction := i%3 - 1; direction != 0 {
			wantVotes["voter-b"] = strconv.Itoa(direction)
		}
		if err != nil || !reflect.DeepEqual(votes, wantVotes) {
			t.Errorf("帖子 %s 投票 = %v, err = %v, want %v", postID, votes, err, wantVotes)
		}
	}
	deletedID := strconv.FormatUint(uint64(posts[5].ID), 10)
	if exists, err := testRedis.Exists(ctx, "bluebell:post:votes:"+deletedID).Result(); err != nil || exists != 0 {
		t.Errorf("已删除帖子的投票未清理：exists = %d, err = %v", exists, err)
	}
	for _, order := range []string{"time", "hot"} {
		ids, total, err := postStore.FindPostIDs(ctx, nil, order, 0, len(posts))
		if err != nil || total != 3 || len(ids) != 3 {
			t.Errorf("%s 排行 = %v, total = %d, err = %v, want 3 篇成功帖子", order, ids, total, err)
		}
	}
}

func seedOutboxPosts(t *testing.T, n int) []model.Post {
	t.Helper()
	communityID := seedCommunity(t, "Outbox")
	posts := make([]model.Post, n)
	repo := repository.NewPostRepository(testDB)
	for i := range posts {
		posts[i] = model.Post{Title: fmt.Sprintf("帖子 %d", i), Content: "正文", AuthorID: "author", CommunityID: communityID}
		if err := repo.Create(t.Context(), &posts[i]); err != nil {
			t.Fatal(err)
		}
	}
	return posts
}

// 回调只拦截 Worker 查询的帖子；用 channel 控制交错，避免靠执行耗时判断并发。
func onOutboxPostQuery(t *testing.T, fn func(*gorm.DB, *model.Post)) func() {
	t.Helper()
	const name = "test:outbox_post_query"
	if err := testDB.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if post, ok := tx.Statement.Dest.(*model.Post); ok && tx.Statement.Unscoped && tx.Error == nil {
			fn(tx, post)
		}
	}); err != nil {
		t.Fatal(err)
	}
	restore := sync.OnceFunc(func() {
		if err := testDB.Callback().Query().Remove(name); err != nil {
			t.Errorf("移除测试回调失败: %v", err)
		}
	})
	t.Cleanup(restore)
	return restore
}

func findOutboxEvents(t *testing.T) []model.OutboxEvent {
	t.Helper()
	var events []model.OutboxEvent
	if err := testDB.WithContext(t.Context()).Order("id").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	return events
}

func waitOutbox(t *testing.T, ctx context.Context, ready func() bool) {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for !ready() {
		select {
		case <-ctx.Done():
			t.Fatal("等待 Outbox 状态超时")
		case <-ticker.C:
		}
	}
}
