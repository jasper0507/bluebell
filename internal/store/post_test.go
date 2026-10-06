package store

import (
	"testing"
	"time"
)

func TestVote_Direction(t *testing.T) {
	reset(t)

	postStore := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		steps    []int8
		wantUp   int64
		wantDown int64
	}{
		{name: "赞成", steps: []int8{1}, wantUp: 1},
		{name: "反对", steps: []int8{-1}, wantDown: 1},
		{name: "赞成后取消", steps: []int8{1, 0}},
		{name: "赞成改反对", steps: []int8{1, -1}, wantDown: 1},
		{name: "重复赞成", steps: []int8{1, 1}, wantUp: 1},
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

			stats, err := postStore.FindVoteStatsByPostIDs(ctx, []uint{postID})
			if err != nil {
				t.Fatalf("查询票数失败: %v", err)
			}
			if stats[postID].UpVotes != tt.wantUp || stats[postID].DownVotes != tt.wantDown {
				t.Fatalf("up = %d down = %d, want up %d down %d", stats[postID].UpVotes, stats[postID].DownVotes, tt.wantUp, tt.wantDown)
			}
		})
	}
}

func TestInitPost_PreservesVotesOnReplay(t *testing.T) {
	reset(t)

	ctx := t.Context()
	posts := NewPostStore(testRedis)
	createdAt := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	const postID uint = 21
	communityID := uint(3)
	if err := posts.InitPost(ctx, postID, communityID, createdAt); err != nil {
		t.Fatalf("初始化帖子失败: %v", err)
	}
	if err := posts.ApplyVote(ctx, postID, communityID, "voter", 1, createdAt); err != nil {
		t.Fatalf("投票失败: %v", err)
	}
	if err := posts.InitPost(ctx, postID, communityID, createdAt); err != nil {
		t.Fatalf("重复初始化失败: %v", err)
	}
	stats, err := posts.FindVoteStatsByPostIDs(ctx, []uint{postID})
	if err != nil {
		t.Fatalf("查询票数失败: %v", err)
	}
	if got := stats[postID]; got.UpVotes != 1 || got.DownVotes != 0 {
		t.Fatalf("重复初始化后的票数 = %+v, want 1 赞成、0 反对", got)
	}
	// 重放同一投票也不能再次计票。
	if err := posts.ApplyVote(ctx, postID, communityID, "voter", 1, createdAt); err != nil {
		t.Fatalf("重放投票失败: %v", err)
	}
	stats, err = posts.FindVoteStatsByPostIDs(ctx, []uint{postID})
	if err != nil {
		t.Fatalf("查询票数失败: %v", err)
	}
	if got := stats[postID]; got.UpVotes != 1 || got.DownVotes != 0 {
		t.Fatalf("重放投票后的票数 = %+v, want 1 赞成、0 反对", got)
	}
	assertPostOrder(t, posts, nil, "hot", postID)
	assertPostOrder(t, posts, &communityID, "time", postID)
}

func TestVote_HotOutranksNewerPost(t *testing.T) {
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
