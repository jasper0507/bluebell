package service

import (
	"errors"
	"testing"
	"time"

	"github.com/jasper0507/bluebell/internal/model"
)

func TestPostService_VoteClosesAfterSevenDays(t *testing.T) {
	reset(t)

	ctx := t.Context()
	userRepo, users, posts := newPostService(t)
	communityID := seedCommunity(t, "Go")
	authorID := registerUser(t, users, userRepo, "amy01", "password1")

	postID, err := posts.Create(ctx, "标题", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("创建帖子失败: %v", err)
	}
	if err := posts.Vote(ctx, authorID, postID, 1); err != nil {
		t.Fatalf("赞成失败: %v", err)
	}

	direction, err := posts.GetVote(ctx, authorID, postID)
	if err != nil || direction != 1 {
		t.Fatalf("投票状态 = %d, %v", direction, err)
	}
	detail, err := posts.Detail(ctx, postID)
	if err != nil {
		t.Fatalf("查询详情失败: %v", err)
	}
	if detail.UpVotes != 1 || detail.DownVotes != 0 {
		t.Fatalf("票数 = up %d down %d", detail.UpVotes, detail.DownVotes)
	}

	closedAt := time.Now().Add(-8 * 24 * time.Hour)
	result := testDB.Model(&model.Post{}).Where("id = ?", postID).Update("created_at", closedAt)
	if result.Error != nil || result.RowsAffected != 1 {
		t.Fatalf("回拨创建时间失败: rows=%d err=%v", result.RowsAffected, result.Error)
	}
	if err := posts.Vote(ctx, authorID, postID, 0); !errors.Is(err, ErrVoteClosed) {
		t.Fatalf("超过 7 天后投票 err = %v", err)
	}

	direction, err = posts.GetVote(ctx, authorID, postID)
	if err != nil || direction != 1 {
		t.Fatalf("截止后的投票状态 = %d, %v", direction, err)
	}
	detail, err = posts.Detail(ctx, postID)
	if err != nil {
		t.Fatalf("再次查询详情失败: %v", err)
	}
	if detail.UpVotes != 1 || detail.DownVotes != 0 {
		t.Fatalf("截止后的票数 = up %d down %d", detail.UpVotes, detail.DownVotes)
	}

	if err := posts.Vote(ctx, authorID, 99999999, 1); !errors.Is(err, ErrPostNotFound) {
		t.Fatalf("帖子不存在 err = %v", err)
	}
}
