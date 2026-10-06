package service

import (
	"errors"
	"testing"
)

func TestPost_CreateAndDeleteSyncRanking(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostService(t)
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
	if item.ID != postID || item.Title != "Go 并发" || item.AuthorName != "author01" || item.CommunityName != "Go" {
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
	userRepo, users, posts := newPostService(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	communityID := seedCommunity(t, "Go")
	postID, err := posts.Create(ctx, "投票测试", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}
	// 发帖通知和连续投票通知一起处理，按 MySQL 中的最终状态计票。
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
			syncOutbox(t)
			direction, err := posts.GetVote(ctx, authorID, postID)
			if err != nil {
				t.Fatalf("查询投票状态失败: %v", err)
			}
			if direction != tt.wantDir {
				t.Fatalf("投票方向 = %d, want %d", direction, tt.wantDir)
			}
			detail, err := posts.Detail(ctx, postID)
			if err != nil {
				t.Fatalf("查询帖子详情失败: %v", err)
			}
			if detail.UpVotes != tt.wantUp || detail.DownVotes != tt.wantDown {
				t.Fatalf("赞成 = %d, 反对 = %d, want %d/%d", detail.UpVotes, detail.DownVotes, tt.wantUp, tt.wantDown)
			}
		})
	}
}
