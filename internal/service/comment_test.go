package service

import (
	"errors"
	"testing"

	"github.com/jasper0507/bluebell/internal/repository"
)

func TestComment_ReplyWithinPostAndDelete(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostService(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	communityID := seedCommunity(t, "Go")
	postID, err := posts.Create(ctx, "讨论帖", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}
	otherPostID, err := posts.Create(ctx, "另一个帖子", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("创建另一个帖子失败: %v", err)
	}
	comments := NewCommentService(
		repository.NewCommentRepository(testDB),
		repository.NewPostRepository(testDB),
		userRepo,
	)
	commentID, err := comments.Create(ctx, postID, authorID, "第一条评论", nil)
	if err != nil {
		t.Fatalf("评论失败: %v", err)
	}
	replyID, err := comments.Create(ctx, postID, authorID, "同帖回复", &commentID)
	if err != nil {
		t.Fatalf("同帖回复失败: %v", err)
	}
	if _, err := comments.Create(ctx, otherPostID, authorID, "跨帖回复", &commentID); !errors.Is(err, ErrInvalidReplyTarget) {
		t.Fatalf("跨帖回复 err = %v, want ErrInvalidReplyTarget", err)
	}

	items, total, err := comments.List(ctx, postID, 2, 1)
	if err != nil {
		t.Fatalf("查询评论失败: %v", err)
	}
	if total != 2 || len(items) != 1 {
		t.Fatalf("第二页 = %+v, total = %d, want 1 条回复、共 2 条评论", items, total)
	}
	reply := items[0]
	if reply.ID != replyID || reply.Content != "同帖回复" || reply.AuthorName != "author01" || reply.ReplyToCommentID == nil || *reply.ReplyToCommentID != commentID {
		t.Fatalf("回复信息不匹配: %+v", reply)
	}
	if err := comments.Delete(ctx, replyID, "another-user"); !errors.Is(err, ErrCommentForbidden) {
		t.Fatalf("非作者删除评论 err = %v, want ErrCommentForbidden", err)
	}
	if err := comments.Delete(ctx, replyID, authorID); err != nil {
		t.Fatalf("作者删除评论失败: %v", err)
	}
	items, total, err = comments.List(ctx, postID, 1, 10)
	if err != nil {
		t.Fatalf("删除后查询评论失败: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].ID != commentID {
		t.Fatalf("删除后列表 = %+v, total = %d, want 只剩原评论", items, total)
	}
	items, total, err = comments.List(ctx, otherPostID, 1, 10)
	if err != nil {
		t.Fatalf("查询另一个帖子的评论失败: %v", err)
	}
	if total != 0 || len(items) != 0 {
		t.Fatalf("另一个帖子的评论 = %+v, total = %d, want 空列表", items, total)
	}
}
