package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/store"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func TestOutbox_BatchQueriesAndBatchAck(t *testing.T) {
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
		// 另一用户有状态但没有通知，批量查询必须按 (postID, userID) 精确匹配。
		decoy := model.PostVote{PostID: post.ID, UserID: fmt.Sprintf("voter-%d", (i+1)%2), Direction: -1}
		if err := testDB.Create(&decoy).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 同帖重复通知应合并，帖子与用户都只查询一次。
	extra := model.OutboxEvent{EventType: model.EventPostIndexSync, AggregateID: posts[0].ID}
	if err := testDB.Create(&extra).Error; err != nil {
		t.Fatal(err)
	}
	wantEvents := findOutboxEvents(t)

	started := make(chan []model.Post, 1)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	var postQueries, voteQueries atomic.Int32
	onOutboxQuery(t, func(tx *gorm.DB) {
		switch rows := tx.Statement.Dest.(type) {
		case *[]model.Post:
			postQueries.Add(1)
			select {
			case started <- *rows:
			case <-tx.Statement.Context.Done():
				tx.AddError(tx.Statement.Context.Err())
				return
			}
			select {
			case <-release:
			case <-tx.Statement.Context.Done():
				tx.AddError(tx.Statement.Context.Err())
			}
		case *[]model.PostVote:
			voteQueries.Add(1)
		}
	})
	ctx, cancel, done := startOutboxWorker(t)
	select {
	case batch := <-started:
		gotIDs := make([]uint, len(batch))
		wantIDs := make([]uint, len(posts))
		for i, post := range batch {
			gotIDs[i] = post.ID
		}
		for i, post := range posts {
			wantIDs[i] = post.ID
		}
		slices.Sort(gotIDs)
		slices.Sort(wantIDs)
		if !slices.Equal(gotIDs, wantIDs) {
			t.Fatalf("批量查询帖子 = %v, want %v", gotIDs, wantIDs)
		}
	case <-ctx.Done():
		t.Fatal("等待批量查询超时")
	}
	if got := findOutboxEvents(t); !reflect.DeepEqual(got, wantEvents) {
		t.Fatalf("同步完成前事件发生变化：got %+v, want %+v", got, wantEvents)
	}
	unblock()
	waitOutbox(t, ctx, func() bool { return len(findOutboxEvents(t)) == 0 })
	stopOutboxWorker(t, cancel, done)
	if postQueries.Load() != 1 || voteQueries.Load() != 1 {
		t.Fatalf("帖子查询 %d 次、投票查询 %d 次，want 各 1 次", postQueries.Load(), voteQueries.Load())
	}

	postStore := store.NewPostStore(testRedis)
	for i, post := range posts {
		postID := strconv.FormatUint(uint64(post.ID), 10)
		score, err := testRedis.ZScore(t.Context(), "bluebell:post:vote_scores", postID).Result()
		if err != nil || score != 1 {
			t.Errorf("帖子 %s 净分 = %g, err = %v, want 1", postID, score, err)
		}
		votes, err := testRedis.HGetAll(t.Context(), "bluebell:post:votes:"+postID).Result()
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
	if err := testRedis.ZAdd(ctx, "bluebell:post:vote_scores", redis.Z{
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
		if err != nil || total != 4 || len(ids) != 4 {
			t.Errorf("%s 排行 = %v, total = %d, err = %v, want 4 篇成功帖子", order, ids, total, err)
		}
	}
	// 修复失败帖子的投影后重试；成功帖子的票数不能重复累加。
	if err := testRedis.ZRem(ctx, "bluebell:post:vote_scores", posts[1].ID).Err(); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Model(&model.OutboxEvent{}).Where("aggregate_id = ?", posts[1].ID).
		Update("next_retry_at", gorm.Expr("TIMESTAMPADD(SECOND, -1, CURRENT_TIMESTAMP(3))")).Error; err != nil {
		t.Fatal(err)
	}
	syncOutbox(t)
	for _, i := range []int{0, 1, 2, 3, 4} {
		id := strconv.FormatUint(uint64(posts[i].ID), 10)
		if score, err := testRedis.ZScore(ctx, "bluebell:post:vote_scores", id).Result(); err != nil || score != float64(i%3) {
			t.Errorf("重试后帖子 %s 净分 = %g, err = %v, want %d", id, score, err, i%3)
		}
	}
}

func TestOutbox_MySQLBatchFailurePreservesEvents(t *testing.T) {
	for _, kind := range []string{"帖子缺失", "投票缺失", "帖子查询失败", "投票查询失败"} {
		t.Run(kind, func(t *testing.T) {
			reset(t)
			posts := seedOutboxPosts(t, 3)
			repo := repository.NewPostRepository(testDB)
			for _, post := range posts {
				if err := repo.Vote(t.Context(), post.ID, "voter", 1); err != nil {
					t.Fatal(err)
				}
			}
			vote := model.PostVote{PostID: posts[1].ID, UserID: "voter", Direction: 1}
			table := "posts"
			switch kind {
			case "帖子缺失":
				if err := testDB.Unscoped().Delete(&posts[1]).Error; err != nil {
					t.Fatal(err)
				}
			case "投票缺失":
				table = "post_votes"
				if err := testDB.Delete(&vote).Error; err != nil {
					t.Fatal(err)
				}
			case "投票查询失败":
				table = "post_votes"
			}
			want := findOutboxEvents(t)
			retried := make(chan struct{}, 1)
			release := make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			var queries int
			restore := onOutboxQuery(t, func(tx *gorm.DB) {
				if tx.Statement.Table != table {
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
				if strings.HasSuffix(kind, "查询失败") {
					tx.AddError(errors.New("测试批量查询失败"))
				}
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
			switch kind {
			case "帖子缺失":
				if err := testDB.Create(&posts[1]).Error; err != nil {
					t.Fatal(err)
				}
			case "投票缺失":
				if err := testDB.Create(&vote).Error; err != nil {
					t.Fatal(err)
				}
			}
			syncOutbox(t)
			for _, post := range posts {
				id := strconv.FormatUint(uint64(post.ID), 10)
				if score, err := testRedis.ZScore(t.Context(), "bluebell:post:vote_scores", id).Result(); err != nil || score != 1 {
					t.Errorf("恢复后帖子 %s 净分 = %g, err = %v, want 1", id, score, err)
				}
			}
		})
	}
}

func TestOutbox_DrainsMultipleBatchesWithoutVoteQueries(t *testing.T) {
	reset(t)
	posts := seedOutboxPosts(t, 257)
	batches := make(chan int, 2)
	var voteQueries atomic.Int32
	onOutboxQuery(t, func(tx *gorm.DB) {
		switch rows := tx.Statement.Dest.(type) {
		case *[]model.Post:
			select {
			case batches <- len(*rows):
			case <-tx.Statement.Context.Done():
				tx.AddError(tx.Statement.Context.Err())
			}
		case *[]model.PostVote:
			voteQueries.Add(1)
		}
	})
	syncOutbox(t)
	for _, want := range []int{256, 1} {
		select {
		case got := <-batches:
			if got != want {
				t.Fatalf("批量帖子数量 = %d, want %d", got, want)
			}
		default:
			t.Fatalf("缺少 %d 个帖子的批量查询", want)
		}
	}
	if got := voteQueries.Load(); got != 0 {
		t.Fatalf("无投票通知仍查询投票 %d 次", got)
	}
	ids, total, err := store.NewPostStore(testRedis).FindPostIDs(t.Context(), nil, "time", 0, len(posts))
	if err != nil || len(ids) != len(posts) || total != int64(len(posts)) {
		t.Fatalf("跨批次同步后排行 = %v, total = %d, err = %v", ids, total, err)
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
