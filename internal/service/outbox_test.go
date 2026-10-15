package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jasper0507/ripple/internal/model"
	"github.com/jasper0507/ripple/internal/repository"
	"github.com/jasper0507/ripple/internal/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// 同批多帖多用户：投票按 (postID, userID) 精确匹配，同帖重复通知合并后全部确认。
func TestOutbox_SyncsCoalescedEvents(t *testing.T) {
	reset(t)
	posts := seedOutboxPosts(t, 9)
	postRepo := repository.NewPostRepository(testDB)
	for i, post := range posts {
		userID := fmt.Sprintf("voter-%d", i%2)
		for _, direction := range []int8{-1, 0, 1} {
			if err := postRepo.Vote(t.Context(), post.ID, userID, direction); err != nil {
				t.Fatal(err)
			}
		}
		// 另一用户有投票状态但没有通知，不能被同步到 Redis。
		decoy := model.PostVote{PostID: post.ID, UserID: fmt.Sprintf("voter-%d", (i+1)%2), Direction: -1}
		if err := testDB.Create(&decoy).Error; err != nil {
			t.Fatal(err)
		}
	}
	extra := model.OutboxEvent{EventType: model.EventPostIndexSync, AggregateID: posts[0].ID}
	if err := testDB.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}

	syncOutbox(t)

	postStore := store.NewPostStore(testRedis)
	for i, post := range posts {
		postID := strconv.FormatUint(uint64(post.ID), 10)
		score, err := testRedis.ZScore(t.Context(), "ripple:post:vote_scores", postID).Result()
		if err != nil || score != 1 {
			t.Errorf("帖子 %s 净分 = %g, err = %v, want 1", postID, score, err)
		}
		votes, err := testRedis.HGetAll(t.Context(), "ripple:post:votes:"+postID).Result()
		want := map[string]string{fmt.Sprintf("voter-%d", i%2): "1"}
		if err != nil || !reflect.DeepEqual(votes, want) {
			t.Errorf("帖子 %s 投票 = %v, err = %v, want %v", postID, votes, err, want)
		}
	}
	for _, scope := range []*uint{nil, &posts[0].CommunityID} {
		for _, order := range []string{"time", "hot"} {
			ids, total, err := postStore.FindPostIDs(t.Context(), scope, order, 0, len(posts))
			if err != nil || total != int64(len(posts)) || len(ids) != len(posts) {
				t.Errorf("%s 排行 = %v, total = %d, err = %v", order, ids, total, err)
			}
		}
	}
}

// 复现此前确认 DELETE 扫描到集合外未提交事件的锁等待。
// 缺失目标也要覆盖：LIMIT 在这种情况下仍可能扫描集合外记录。
func TestOutbox_DeleteByIDsDoesNotWaitForUnrelatedInsert(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(fmt.Sprintf("missing_target=%t", missing), func(t *testing.T) {
			reset(t)
			posts := seedOutboxPosts(t, 20)
			events := findOutboxEvents(t)
			ids := make([]uint64, len(events))
			for i, event := range events {
				ids[i] = event.ID
			}
			if missing {
				if err := testDB.Delete(&events[0]).Error; err != nil {
					t.Fatal(err)
				}
			}
			tx := testDB.WithContext(t.Context()).Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			held := model.OutboxEvent{EventType: model.EventPostIndexSync, AggregateID: posts[0].ID}
			if err := tx.Create(&held).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.First(&held, held.ID).Error; err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if err := repository.NewOutboxRepository(testDB).DeleteByIDs(ctx, ids); err != nil {
				t.Fatalf("确认等待集合外未提交事件 %d: %v", held.ID, err)
			}
			if err := tx.Commit().Error; err != nil {
				t.Fatal(err)
			}
			if got := findOutboxEvents(t); !reflect.DeepEqual(got, []model.OutboxEvent{held}) {
				t.Fatalf("集合外事件未保留：got %+v, want %+v", got, held)
			}
		})
	}
}

func TestOutbox_CancelWaitsForBatchAndPreservesEvents(t *testing.T) {
	reset(t)
	seedOutboxPosts(t, 9)
	want := findOutboxEvents(t)

	started := make(chan struct{}, 1)
	cancelled := make(chan struct{}, 1)
	release := make(chan struct{})
	// 失败路径也要释放回调，避免测试清理被阻塞。
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	restore := onOutboxQuery(t, func(tx *gorm.DB) {
		if tx.Statement.Table != "posts" {
			return
		}
		started <- struct{}{}
		<-tx.Statement.Context.Done()
		cancelled <- struct{}{}
		<-release
		tx.AddError(tx.Statement.Context.Err())
	})
	ctx, cancel, done := startOutboxWorker(t)
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("等待批量查询启动超时")
	}
	cancel()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("批量查询未收到取消信号")
	}
	select {
	case <-done:
		t.Fatal("Worker 在批量查询结束前返回")
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
	// 一帖投影不完整，失败仅归属该帖，不影响同批其他帖子。
	if err := testRedis.ZAdd(ctx, "ripple:post:vote_scores", redis.Z{
		Member: strconv.FormatUint(uint64(posts[1].ID), 10), Score: 0,
	}).Err(); err != nil {
		t.Fatal(err)
	}
	// 已软删除的帖子应清理投影并确认同帖所有事件。
	postStore := store.NewPostStore(testRedis)
	if failed := postStore.SyncPosts(ctx, []store.PostSync{{
		PostID: posts[5].ID, CommunityID: posts[5].CommunityID, CreatedAt: posts[5].CreatedAt,
		NeedIndexSync: true, Votes: map[string]int8{"voter-a": 1},
	}}); len(failed) != 0 {
		t.Fatalf("初始化待删除帖子失败: %v", failed)
	}
	if err := postRepo.Delete(ctx, posts[5].ID); err != nil {
		t.Fatal(err)
	}

	wantFailed := make(map[uint64]model.OutboxEvent)
	for _, event := range findOutboxEvents(t) {
		if event.AggregateID == posts[1].ID {
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
	}
	for _, i := range []int{0, 2, 3, 4} {
		postID := strconv.FormatUint(uint64(posts[i].ID), 10)
		score, err := testRedis.ZScore(ctx, "ripple:post:vote_scores", postID).Result()
		if want := float64(i % 3); err != nil || score != want {
			t.Errorf("帖子 %s 净分 = %g, err = %v, want %g", postID, score, err, want)
		}
		votes, err := testRedis.HGetAll(ctx, "ripple:post:votes:"+postID).Result()
		wantVotes := map[string]string{"voter-a": "1"}
		if direction := i%3 - 1; direction != 0 {
			wantVotes["voter-b"] = strconv.Itoa(direction)
		}
		if err != nil || !reflect.DeepEqual(votes, wantVotes) {
			t.Errorf("帖子 %s 投票 = %v, err = %v, want %v", postID, votes, err, wantVotes)
		}
	}
	deletedID := strconv.FormatUint(uint64(posts[5].ID), 10)
	if exists, err := testRedis.Exists(ctx, "ripple:post:votes:"+deletedID).Result(); err != nil || exists != 0 {
		t.Errorf("已删除帖子的投票未清理：exists = %d, err = %v", exists, err)
	}
	for _, order := range []string{"time", "hot"} {
		ids, total, err := postStore.FindPostIDs(ctx, nil, order, 0, len(posts))
		if err != nil || total != 4 || len(ids) != 4 {
			t.Errorf("%s 排行 = %v, total = %d, err = %v, want 4 篇成功帖子", order, ids, total, err)
		}
	}
	// 修复失败帖子的投影后重试；成功帖子的票数不能重复累加。
	if err := testRedis.ZRem(ctx, "ripple:post:vote_scores", posts[1].ID).Err(); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", posts[1].ID).
		Update("next_retry_at", gorm.Expr("TIMESTAMPADD(SECOND, -1, CURRENT_TIMESTAMP(3))")).Error; err != nil {
		t.Fatal(err)
	}
	syncOutbox(t)
	for _, i := range []int{0, 1, 2, 3, 4} {
		id := strconv.FormatUint(uint64(posts[i].ID), 10)
		if score, err := testRedis.ZScore(ctx, "ripple:post:vote_scores", id).Result(); err != nil || score != float64(i%3) {
			t.Errorf("重试后帖子 %s 净分 = %g, err = %v, want %d", id, score, err, i%3)
		}
	}
}

// MySQL 查询失败时整批不确认、不记重试、不写 Redis，恢复后正常同步。
func TestOutbox_MySQLBatchFailurePreservesEvents(t *testing.T) {
	reset(t)
	posts := seedOutboxPosts(t, 3)
	repo := repository.NewPostRepository(testDB)
	for _, post := range posts {
		if err := repo.Vote(t.Context(), post.ID, "voter", 1); err != nil {
			t.Fatal(err)
		}
	}
	want := findOutboxEvents(t)
	retried := make(chan struct{}, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var queries int
	restore := onOutboxQuery(t, func(tx *gorm.DB) {
		if tx.Statement.Table != "post_votes" {
			return
		}
		queries++
		// 第二次查询说明第一批已经失败并进入轮询重试。
		if queries == 2 {
			retried <- struct{}{}
			select {
			case <-release:
			case <-tx.Statement.Context.Done():
			}
		}
		tx.AddError(errors.New("测试投票查询失败"))
	})
	ctx, cancel, done := startOutboxWorker(t)
	select {
	case <-retried:
	case <-ctx.Done():
		t.Fatal("等待整批查询失败后的重试超时")
	}
	if got := findOutboxEvents(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("MySQL 整批失败后事件被确认或重试字段被修改：got %+v, want %+v", got, want)
	}
	if size, err := testRedis.DBSize(t.Context()).Result(); err != nil || size != 0 {
		t.Fatalf("MySQL 整批失败仍写入 Redis：size = %d, err = %v", size, err)
	}
	cancel()
	unblock()
	stopOutboxWorker(t, cancel, done)
	restore()

	syncOutbox(t)
	for _, post := range posts {
		id := strconv.FormatUint(uint64(post.ID), 10)
		if score, err := testRedis.ZScore(t.Context(), "ripple:post:vote_scores", id).Result(); err != nil || score != 1 {
			t.Errorf("恢复后帖子 %s 净分 = %g, err = %v, want 1", id, score, err)
		}
	}
}

// 非法事件只退避重试自身，同批正常事件照常确认，Worker 不退出。
func TestOutbox_InvalidEventsRetryWithoutBlockingOthers(t *testing.T) {
	reset(t)
	posts := seedOutboxPosts(t, 1)
	emptyPayload := `{}`
	invalid := []model.OutboxEvent{
		{EventType: model.EventPostVoteSync, AggregateID: posts[0].ID},
		{EventType: model.EventPostVoteSync, AggregateID: posts[0].ID, Payload: &emptyPayload},
		{EventType: "post.unknown", AggregateID: posts[0].ID},
	}
	if err := testDB.Create(&invalid).Error; err != nil {
		t.Fatal(err)
	}

	ctx, cancel, done := startOutboxWorker(t)
	waitOutbox(t, ctx, func() bool { return len(findOutboxEvents(t)) == len(invalid) })
	stopOutboxWorker(t, cancel, done)

	for _, event := range findOutboxEvents(t) {
		if event.RetryCount != 1 || event.LastError == "" || !event.NextRetryAt.After(event.CreatedAt) {
			t.Errorf("非法事件 %d 未安排重试: retry=%d, next=%v, created=%v, err=%q",
				event.ID, event.RetryCount, event.NextRetryAt, event.CreatedAt, event.LastError)
		}
	}
	ids, total, err := store.NewPostStore(testRedis).FindPostIDs(t.Context(), nil, "time", 0, 10)
	if err != nil || total != 1 || len(ids) != 1 || ids[0] != posts[0].ID {
		t.Fatalf("正常事件未同步: ids=%v, total=%d, err=%v", ids, total, err)
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

// 只拦截 Worker 的批量帖子状态查询及投票查询。
func onOutboxQuery(t *testing.T, fn func(*gorm.DB)) func() {
	t.Helper()
	const name = "test:outbox_query"
	if err := testDB.Callback().Query().After("gorm:query").Register(name, func(tx *gorm.DB) {
		if tx.Error == nil && ((tx.Statement.Table == "posts" && tx.Statement.Unscoped) || tx.Statement.Table == "post_votes") {
			fn(tx)
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
