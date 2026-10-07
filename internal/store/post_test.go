package store

import (
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestPostStore_ApplyVote_Direction(t *testing.T) {
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
			ctx := t.Context()
			postID := uint(1000 + i)
			const communityID uint = 1

			if err := postStore.InitPost(ctx, postID, communityID, createdAt); err != nil {
				t.Fatalf("初始化帖子失败: %v", err)
			}
			for _, direction := range tt.steps {
				if err := postStore.ApplyVote(
					ctx, postID, communityID, "voter", direction, createdAt,
				); err != nil {
					t.Fatalf("投票 %d 失败: %v", direction, err)
				}
			}

			assertVoteDirection(t, postID, "voter", tt.wantDir)
			assertVoteProjection(t, postID, communityID, createdAt, tt.wantScore)
		})
	}
}

func TestPostStore_InitPost_PreservesVotesOnReplay(t *testing.T) {
	reset(t)

	ctx := t.Context()
	posts := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	const postID uint = 21
	communityID := uint(3)
	if err := posts.InitPost(ctx, postID, communityID, createdAt); err != nil {
		t.Fatalf("初始化帖子失败: %v", err)
	}
	for _, userID := range []string{"voter", "another-voter"} {
		if err := posts.ApplyVote(ctx, postID, communityID, userID, 1, createdAt); err != nil {
			t.Fatalf("%s 投票失败: %v", userID, err)
		}
	}
	if err := posts.InitPost(ctx, postID, communityID, createdAt); err != nil {
		t.Fatalf("重复初始化失败: %v", err)
	}
	assertVoteDirection(t, postID, "voter", 1)
	assertVoteDirection(t, postID, "another-voter", 1)
	assertVoteProjection(t, postID, communityID, createdAt, 2)
	// 重放同一投票也不能再次计票。
	if err := posts.ApplyVote(ctx, postID, communityID, "voter", 1, createdAt); err != nil {
		t.Fatalf("重放投票失败: %v", err)
	}
	assertVoteDirection(t, postID, "voter", 1)
	assertVoteProjection(t, postID, communityID, createdAt, 2)
	assertPostOrder(t, posts, nil, "hot", postID)
	assertPostOrder(t, posts, &communityID, "time", postID)
}

func TestPostStore_ApplyVote_MultipleUsers(t *testing.T) {
	reset(t)

	ctx := t.Context()
	posts := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	const postID, communityID uint = 21, 3
	if err := posts.InitPost(ctx, postID, communityID, createdAt); err != nil {
		t.Fatalf("初始化帖子失败: %v", err)
	}

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
		if err := posts.ApplyVote(ctx, postID, communityID, tt.userID, tt.direction, createdAt); err != nil {
			t.Fatalf("%s 投票 %d 失败: %v", tt.userID, tt.direction, err)
		}
		assertVoteDirection(t, postID, tt.userID, tt.direction)
		assertVoteProjection(t, postID, communityID, createdAt, tt.wantScore)
	}
}

func TestPostStore_ApplyVote_HotOutranksNewerPost(t *testing.T) {
	reset(t)

	ctx := t.Context()
	postStore := NewPostStore(testRedis)
	olderAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	newerAt := olderAt.Add(time.Hour)

	const (
		olderID uint = 11
		newerID uint = 12
	)
	olderCommunity := uint(3)
	newerCommunity := uint(4)

	if err := postStore.InitPost(ctx, olderID, olderCommunity, olderAt); err != nil {
		t.Fatalf("初始化旧帖失败: %v", err)
	}
	if err := postStore.InitPost(ctx, newerID, newerCommunity, newerAt); err != nil {
		t.Fatalf("初始化新帖失败: %v", err)
	}
	for _, userID := range []string{"u1", "u2", "u3"} {
		if err := postStore.ApplyVote(ctx, olderID, olderCommunity, userID, 1, olderAt); err != nil {
			t.Fatalf("%s 投票失败: %v", userID, err)
		}
	}

	assertPostOrder(t, postStore, nil, "time", newerID, olderID)
	assertPostOrder(t, postStore, nil, "hot", olderID, newerID)
	assertPostOrder(t, postStore, &olderCommunity, "hot", olderID)
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
