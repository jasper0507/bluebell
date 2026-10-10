package store

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestPostStore_SyncPosts_VoteDirection(t *testing.T) {
	reset(t)

	postStore := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		steps     []int8
		wantDir   int8
		wantScore float64
	}{
		{name: "未投票取消", steps: []int8{0}},
		{name: "赞成", steps: []int8{1}, wantDir: 1, wantScore: 1},
		{name: "反对", steps: []int8{-1}, wantDir: -1, wantScore: -1},
		{name: "赞成后取消", steps: []int8{1, 0}},
		{name: "反对后取消", steps: []int8{-1, 0}},
		{name: "赞成改反对", steps: []int8{1, -1}, wantDir: -1, wantScore: -1},
		{name: "反对改赞成", steps: []int8{-1, 1}, wantDir: 1, wantScore: 1},
		{name: "重复赞成", steps: []int8{1, 1}, wantDir: 1, wantScore: 1},
		{name: "重复反对", steps: []int8{-1, -1}, wantDir: -1, wantScore: -1},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			postID := uint(1000 + i)
			const communityID uint = 1

			post := PostSync{PostID: postID, CommunityID: communityID, CreatedAt: createdAt, NeedIndexSync: true}
			assertSyncPosts(t, postStore, post)
			post.NeedIndexSync = false
			for _, direction := range tt.steps {
				post.Votes = map[string]int8{"voter": direction}
				assertSyncPosts(t, postStore, post)
			}

			assertVoteDirection(t, postID, "voter", tt.wantDir)
			assertVoteProjection(t, postID, communityID, createdAt, tt.wantScore)
		})
	}
}

func TestPostStore_SyncPosts_PreservesVotesOnReplay(t *testing.T) {
	reset(t)

	posts := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	const postID uint = 21
	communityID := uint(3)
	post := PostSync{
		PostID: postID, CommunityID: communityID, CreatedAt: createdAt, NeedIndexSync: true,
		Votes: map[string]int8{"voter": 1, "another-voter": 1},
	}
	assertSyncPosts(t, posts, post)
	assertSyncPosts(t, posts, post)
	post.Votes = nil
	assertSyncPosts(t, posts, post)
	assertVoteDirection(t, postID, "voter", 1)
	assertVoteDirection(t, postID, "another-voter", 1)
	assertVoteProjection(t, postID, communityID, createdAt, 2)
	// 重放同一投票也不能再次计票。
	post.NeedIndexSync = false
	post.Votes = map[string]int8{"voter": 1}
	assertSyncPosts(t, posts, post)
	assertVoteDirection(t, postID, "voter", 1)
	assertVoteProjection(t, postID, communityID, createdAt, 2)
	assertPostOrder(t, posts, nil, "hot", postID)
	assertPostOrder(t, posts, &communityID, "time", postID)
}

func TestPostStore_SyncPosts_MultipleUsers(t *testing.T) {
	reset(t)

	posts := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	const postID, communityID uint = 21, 3
	post := PostSync{PostID: postID, CommunityID: communityID, CreatedAt: createdAt, NeedIndexSync: true}
	assertSyncPosts(t, posts, post)
	post.NeedIndexSync = false

	for _, tt := range []struct {
		userID    string
		direction int8
		wantScore float64
	}{
		{userID: "u1", direction: 1, wantScore: 1},
		{userID: "u2", direction: 1, wantScore: 2},
		{userID: "u3", direction: -1, wantScore: 1},
		{userID: "u1", direction: -1, wantScore: -1},
		{userID: "u2", direction: 0, wantScore: -2},
		{userID: "u3", direction: 0, wantScore: -1},
		{userID: "u1", direction: 1, wantScore: 1},
	} {
		post.Votes = map[string]int8{tt.userID: tt.direction}
		assertSyncPosts(t, posts, post)
		assertVoteDirection(t, postID, tt.userID, tt.direction)
		assertVoteProjection(t, postID, communityID, createdAt, tt.wantScore)
	}
	for _, tt := range []struct {
		votes     map[string]int8
		wantScore float64
	}{
		{votes: map[string]int8{"u1": -1, "u2": 1, "u3": -1}, wantScore: -1},
		{votes: map[string]int8{"u1": 0, "u2": 1, "u3": 0}, wantScore: 1},
		{votes: map[string]int8{"u1": 0, "u2": 0, "u3": 0}},
	} {
		post.Votes = tt.votes
		assertSyncPosts(t, posts, post)
		for userID, direction := range tt.votes {
			assertVoteDirection(t, postID, userID, direction)
		}
		assertVoteProjection(t, postID, communityID, createdAt, tt.wantScore)
	}
}

func TestPostStore_SyncPosts_HotOutranksNewerPost(t *testing.T) {
	reset(t)

	postStore := NewPostStore(testRedis)
	olderAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newerAt := olderAt.Add(time.Hour)

	const (
		olderID uint = 11
		newerID uint = 12
	)
	olderCommunity := uint(3)
	newerCommunity := uint(4)

	assertSyncPosts(t, postStore,
		PostSync{PostID: olderID, CommunityID: olderCommunity, CreatedAt: olderAt, NeedIndexSync: true, Votes: map[string]int8{"u1": 1, "u2": 1, "u3": 1}},
		PostSync{PostID: newerID, CommunityID: newerCommunity, CreatedAt: newerAt, NeedIndexSync: true},
	)
	assertVoteProjection(t, olderID, olderCommunity, olderAt, 3)
	assertVoteProjection(t, newerID, newerCommunity, newerAt, 0)

	assertPostOrder(t, postStore, nil, "time", newerID, olderID)
	assertPostOrder(t, postStore, nil, "hot", olderID, newerID)
	assertPostOrder(t, postStore, &olderCommunity, "hot", olderID)
}

func TestPostStore_SyncPosts_Empty(t *testing.T) {
	// 空批次不访问 Redis。
	if failed := NewPostStore(nil).SyncPosts(t.Context(), nil); len(failed) != 0 {
		t.Fatalf("空批次失败结果 = %v", failed)
	}
}

func TestPostStore_SyncPosts_MixedResults(t *testing.T) {
	reset(t)
	ctx := t.Context()
	posts := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	// 只存在净分的帖子不能被当作完整投影初始化。
	if err := testRedis.ZAdd(ctx, postVoteScoreKey, redis.Z{Member: "22", Score: 7}).Err(); err != nil {
		t.Fatal(err)
	}
	syncs := []PostSync{
		{PostID: 21, CommunityID: 3, CreatedAt: createdAt, Votes: map[string]int8{"voter": 1}},
		{PostID: 22, CommunityID: 3, CreatedAt: createdAt, NeedIndexSync: true, Votes: map[string]int8{"voter": 1}},
		{PostID: 23, CommunityID: 4, CreatedAt: createdAt, NeedIndexSync: true, Votes: map[string]int8{"u1": 1, "u2": -1, "u3": 1}},
	}
	failed := posts.SyncPosts(ctx, syncs)
	if len(failed) != 2 || !errors.Is(failed[21], ErrPostProjectionNotInitialized) || failed[22] == nil {
		t.Fatalf("批量失败结果 = %v, want 帖子 21 未初始化、帖子 22 投影不完整", failed)
	}
	assertVoteDirection(t, 21, "voter", 0)
	assertVoteDirection(t, 22, "voter", 0)
	if score, err := testRedis.ZScore(ctx, postVoteScoreKey, "22").Result(); err != nil || score != 7 {
		t.Fatalf("失败帖子原净分被修改：score = %g, err = %v", score, err)
	}
	assertVoteDirection(t, 23, "u1", 1)
	assertVoteDirection(t, 23, "u2", -1)
	assertVoteDirection(t, 23, "u3", 1)
	assertVoteProjection(t, 23, 4, createdAt, 1)

	// 一个帖子失败不影响后续命令；重放成功项不重复计票。
	assertSyncPosts(t, posts, syncs[2])
	assertVoteProjection(t, 23, 4, createdAt, 1)
}

func TestPostStore_SyncPosts_Delete(t *testing.T) {
	reset(t)
	posts := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	deleted := PostSync{PostID: 21, CommunityID: 3, CreatedAt: createdAt, NeedIndexSync: true, Votes: map[string]int8{"voter": 1}}
	survivor := PostSync{PostID: 22, CommunityID: 3, CreatedAt: createdAt, NeedIndexSync: true, Votes: map[string]int8{"voter": -1}}
	assertSyncPosts(t, posts, deleted, survivor)
	// 删除优先于初始化和投票；重复删除也应成功。
	deleted.Deleted = true
	assertSyncPosts(t, posts, deleted, survivor)
	assertSyncPosts(t, posts, deleted)
	assertVoteDirection(t, deleted.PostID, "voter", 0)
	for _, key := range []string{
		postVoteScoreKey,
		postRankKey(nil, "time"), postRankKey(&deleted.CommunityID, "time"),
		postRankKey(nil, "hot"), postRankKey(&deleted.CommunityID, "hot"),
	} {
		if _, err := testRedis.ZScore(t.Context(), key, "21").Result(); err != redis.Nil {
			t.Errorf("删除后 %s 仍有帖子 21：err = %v", key, err)
		}
	}
	assertVoteDirection(t, survivor.PostID, "voter", -1)
	assertVoteProjection(t, survivor.PostID, survivor.CommunityID, createdAt, -1)
	for _, scope := range []*uint{nil, &deleted.CommunityID} {
		for _, order := range []string{"time", "hot"} {
			assertPostOrder(t, posts, scope, order, survivor.PostID)
		}
	}
}

func TestPostStore_SyncPosts_CancelledContext(t *testing.T) {
	reset(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	failed := NewPostStore(testRedis).SyncPosts(ctx, []PostSync{
		{PostID: 21, NeedIndexSync: true},
		{PostID: 22, Deleted: true},
	})
	if len(failed) != 2 {
		t.Fatalf("取消后失败结果 = %v, want 两帖均失败", failed)
	}
	for _, id := range []uint{21, 22} {
		if !errors.Is(failed[id], context.Canceled) {
			t.Errorf("帖子 %d 错误 = %v, want context.Canceled", id, failed[id])
		}
	}
}

func assertSyncPosts(t *testing.T, posts *PostStore, syncs ...PostSync) {
	t.Helper()
	if failed := posts.SyncPosts(t.Context(), syncs); len(failed) != 0 {
		t.Fatalf("同步帖子失败: %v", failed)
	}
}

func assertPostOrder(t *testing.T, postStore *PostStore, communityID *uint, order string, want ...uint) {
	t.Helper()

	ids, total, err := postStore.FindPostIDs(t.Context(), communityID, order, 0, 10)
	if err != nil {
		t.Fatalf("查询 %s 榜失败: %v", order, err)
	}
	if total != int64(len(want)) || len(ids) != len(want) {
		t.Fatalf("%s 榜 = %v total %d, want %v", order, ids, total, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("%s 榜 = %v, want %v", order, ids, want)
		}
	}
}

func assertVoteDirection(t *testing.T, postID uint, userID string, want int8) {
	t.Helper()

	postIDStr := strconv.FormatUint(uint64(postID), 10)
	got, err := testRedis.HGet(t.Context(), postVotesKeyPrefix+postIDStr, userID).Result()
	if want == 0 {
		if err != redis.Nil {
			t.Fatalf("取消后用户 %s 投票 = %q, err = %v, want 字段不存在", userID, got, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("查询用户 %s 投票状态失败: %v", userID, err)
	}
	if got != strconv.FormatInt(int64(want), 10) {
		t.Fatalf("用户 %s 投票方向 = %s, want %d", userID, got, want)
	}
}

func assertVoteProjection(t *testing.T, postID, communityID uint, createdAt time.Time, wantScore float64) {
	t.Helper()

	postIDStr := strconv.FormatUint(uint64(postID), 10)
	score, err := testRedis.ZScore(t.Context(), postVoteScoreKey, postIDStr).Result()
	if err != nil {
		t.Fatalf("查询净投票分失败: %v", err)
	}
	if score != wantScore {
		t.Fatalf("帖子 %d 净投票分 = %g, want %g", postID, score, wantScore)
	}

	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	wantHot := createdAt.Sub(epoch).Seconds() / 45000
	if wantScore > 0 {
		wantHot += math.Log10(math.Max(wantScore, 1))
	} else if wantScore < 0 {
		wantHot -= math.Log10(math.Max(-wantScore, 1))
	}
	for _, scope := range []*uint{nil, &communityID} {
		for order, want := range map[string]float64{
			"hot":  wantHot,
			"time": float64(createdAt.UnixMilli()),
		} {
			key := postRankKey(scope, order)
			got, err := testRedis.ZScore(t.Context(), key, postIDStr).Result()
			if err != nil {
				t.Fatalf("查询 %s 排行分失败: %v", key, err)
			}
			if math.Abs(got-want) > 1e-9 {
				t.Fatalf("%s 排行分 = %g, want %g", key, got, want)
			}
		}
	}
}
