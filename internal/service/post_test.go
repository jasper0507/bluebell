package service

import (
	"errors"
	"math"
	"strconv"
	"testing"
)

func TestPost_ListRejectsInvalidPagination(t *testing.T) {
	// 非法参数必须在访问数据库或 Redis 前被拒绝。
	posts := NewPostService(nil, nil, nil, nil)
	for _, tt := range []struct {
		name     string
		page     int
		pageSize int
	}{
		{name: "页码为零", pageSize: 10},
		{name: "负数页码", page: -1, pageSize: 10},
		{name: "每页数量为零", page: 1},
		{name: "负数每页数量", page: 1, pageSize: -1},
		{name: "页尾超出整数范围", page: math.MaxInt/10 + 1, pageSize: 10},
		{name: "最大整数页码", page: math.MaxInt, pageSize: 10},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := posts.List(t.Context(), tt.page, tt.pageSize, PostOrderByTime, nil)
			if !errors.Is(err, ErrInvalidPagination) {
				t.Fatalf("err = %v, want ErrInvalidPagination", err)
			}
		})
	}
}

func TestPost_CreateAndDeleteSyncRanking(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostTestServices(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	communityID := seedCommunity(t, "Go")

	postID, err := posts.Create(ctx, "Go 并发", "讨论 goroutine", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}
	syncOutbox(t)

	items, total, err := posts.List(ctx, 1, 10, PostOrderByTime, &communityID)
	if err != nil {
		t.Fatalf("查询帖子列表失败: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("列表 = %+v, total = %d, want 1 篇帖子", items, total)
	}
	item := items[0]
	if item.ID != postID || item.Title != "Go 并发" || item.AuthorUsername != "author01" || item.CommunityName != "Go" {
		t.Fatalf("帖子列表信息不匹配: %+v", item)
	}

	if err := posts.Delete(ctx, postID, "another-user"); !errors.Is(err, ErrPostForbidden) {
		t.Fatalf("非作者删帖 err = %v, want ErrPostForbidden", err)
	}
	if err := posts.Delete(ctx, postID, authorID); err != nil {
		t.Fatalf("作者删帖失败: %v", err)
	}
	syncOutbox(t)

	for _, scope := range []*uint{nil, &communityID} {
		for _, order := range []string{PostOrderByTime, PostOrderByHot} {
			items, total, err := posts.List(ctx, 1, 10, order, scope)
			if err != nil {
				t.Fatalf("删除后查询 %s 榜失败: %v", order, err)
			}
			if total != 0 || len(items) != 0 {
				t.Fatalf("删除后 %s 榜 = %+v, total = %d, want 空列表", order, items, total)
			}
		}
	}
	if _, err := posts.Detail(ctx, postID); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("删除后详情 err = %v, want ErrPostNotFound", err)
	}
}

func TestPost_VoteSyncsLatestState(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostTestServices(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	communityID := seedCommunity(t, "Go")
	postID, err := posts.Create(ctx, "投票测试", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}
	assertPostVoteCounts(t, posts, postID, 0, 0)
	// 详情直接读取 MySQL；Outbox 将连续投票的最终状态同步到 Redis 排行。
	for _, tt := range []struct {
		name     string
		steps    []int8
		wantDir  int8
		wantUp   int64
		wantDown int64
	}{
		{name: "重复赞成只计一票", steps: []int8{1, 1}, wantDir: 1, wantUp: 1},
		{name: "多次切换只同步最终反对", steps: []int8{-1, 1, -1}, wantDir: -1, wantDown: 1},
		{name: "取消投票", steps: []int8{0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, direction := range tt.steps {
				if err := posts.Vote(ctx, authorID, postID, direction); err != nil {
					t.Fatalf("投票 %d 失败: %v", direction, err)
				}
			}
			direction, err := posts.GetVote(ctx, authorID, postID)
			if err != nil {
				t.Fatalf("查询投票状态失败: %v", err)
			}
			if direction != tt.wantDir {
				t.Fatalf("投票方向 = %d, want %d", direction, tt.wantDir)
			}
			assertPostVoteCounts(t, posts, postID, tt.wantUp, tt.wantDown)
			syncOutbox(t)
			assertPostVoteCounts(t, posts, postID, tt.wantUp, tt.wantDown)

			score, err := testRedis.ZScore(ctx, "bluebell:post:vote_scores", strconv.FormatUint(uint64(postID), 10)).Result()
			if err != nil {
				t.Fatalf("查询 Redis 净投票分失败: %v", err)
			}
			if want := float64(tt.wantUp - tt.wantDown); score != want {
				t.Fatalf("Redis 净投票分 = %g, want %g", score, want)
			}
		})
	}
}

func TestPost_DetailCountsVotesByPostID(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostTestServices(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	voterID := registerUser(t, users, userRepo, "voter01", "password1")
	cancelledVoterID := registerUser(t, users, userRepo, "voter02", "password1")
	communityID := seedCommunity(t, "Go")
	postID, err := posts.Create(ctx, "投票统计", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}
	otherPostID, err := posts.Create(ctx, "另一篇帖子", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("创建另一篇帖子失败: %v", err)
	}
	for _, vote := range []struct {
		postID    uint
		userID    string
		direction int8
	}{
		{postID: postID, userID: authorID, direction: 1},
		{postID: postID, userID: voterID, direction: -1},
		{postID: postID, userID: cancelledVoterID, direction: 1},
		{postID: postID, userID: cancelledVoterID, direction: 0},
		{postID: otherPostID, userID: authorID, direction: -1},
		{postID: otherPostID, userID: voterID, direction: -1},
	} {
		if err := posts.Vote(ctx, vote.userID, vote.postID, vote.direction); err != nil {
			t.Fatalf("用户 %s 对帖子 %d 投票失败: %v", vote.userID, vote.postID, err)
		}
	}

	// 尚未同步 Redis 时，也应按帖子分别计票，direction = 0 不计入任何票数。
	assertPostVoteCounts(t, posts, postID, 1, 1)
	assertPostVoteCounts(t, posts, otherPostID, 0, 2)
	syncOutbox(t)
	assertPostVoteCounts(t, posts, postID, 1, 1)
	assertPostVoteCounts(t, posts, otherPostID, 0, 2)
}

func assertPostVoteCounts(t *testing.T, posts *PostService, postID uint, wantUp, wantDown int64) {
	t.Helper()

	detail, err := posts.Detail(t.Context(), postID)
	if err != nil {
		t.Fatalf("查询帖子 %d 详情失败: %v", postID, err)
	}
	if detail.UpVotes != wantUp || detail.DownVotes != wantDown {
		t.Errorf("帖子 %d 赞成 = %d, 反对 = %d, want %d/%d", postID, detail.UpVotes, detail.DownVotes, wantUp, wantDown)
	}
}
