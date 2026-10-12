package service

import (
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
)

func newCommentTestService() *CommentService {
	return NewCommentService(
		repository.NewCommentRepository(testDB),
		repository.NewPostRepository(testDB),
	)
}

func TestComment_ListRejectsInvalidPagination(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostTestServices(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	communityID := seedCommunity(t, "Go")
	postID, err := posts.Create(ctx, "分页测试", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}
	comments := newCommentTestService()

	for _, state := range []string{"没有评论", "已有评论"} {
		t.Run(state, func(t *testing.T) {
			if state == "已有评论" {
				if _, err := comments.Create(ctx, postID, authorID, "评论", nil); err != nil {
					t.Fatalf("创建评论失败: %v", err)
				}
			}
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
					_, _, err := comments.List(ctx, postID, tt.page, tt.pageSize)
					if !errors.Is(err, ErrInvalidPagination) {
						t.Fatalf("err = %v, want ErrInvalidPagination", err)
					}
				})
			}
		})
	}

	// 超出实际数据页数但仍在安全范围内，应返回空列表而不是参数错误。
	items, total, err := comments.List(ctx, postID, 2, 10)
	if err != nil {
		t.Fatalf("查询超出数据页数的评论失败: %v", err)
	}
	if total != 1 || len(items) != 0 {
		t.Fatalf("第二页 = %+v, total = %d, want 空列表、共 1 条评论", items, total)
	}
}

func TestComment_ReplyWithinPostAndDelete(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostTestServices(t)
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
	comments := newCommentTestService()
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
	if reply.ID != replyID || reply.Content != "同帖回复" || reply.AuthorUsername != "author01" || reply.ReplyToCommentID == nil || *reply.ReplyToCommentID != commentID {
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

func TestComment_MissingPost(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostTestServices(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	communityID := seedCommunity(t, "Go")
	postID, err := posts.Create(ctx, "待删除", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}
	comments := newCommentTestService()
	if _, err := comments.Create(ctx, postID, authorID, "评论", nil); err != nil {
		t.Fatalf("创建评论失败: %v", err)
	}
	if err := posts.Delete(ctx, postID, authorID); err != nil {
		t.Fatalf("删帖失败: %v", err)
	}

	// 已删除的帖子仍保留评论，用于确认按帖子软删除状态判定，而不是按评论是否存在。
	for name, id := range map[string]uint{"已删除": postID, "不存在": postID + 1000} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := comments.List(ctx, id, 1, 10); !errors.Is(err, ErrPostNotFound) {
				t.Fatalf("查询评论 err = %v, want ErrPostNotFound", err)
			}
			if _, err := comments.Create(ctx, id, authorID, "评论", nil); !errors.Is(err, ErrPostNotFound) {
				t.Fatalf("创建评论 err = %v, want ErrPostNotFound", err)
			}
		})
	}
}

func TestComment_ListFailsWhenAuthorMissing(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostTestServices(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	communityID := seedCommunity(t, "Go")
	postID, err := posts.Create(ctx, "讨论帖", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}
	// 正常写入路径无法产生无作者评论，只能直接写库模拟数据不一致。
	if err := testDB.Create(&model.Comment{
		PostID:   postID,
		AuthorID: "00000000-0000-0000-0000-000000000000",
		Content:  "作者不存在",
	}).Error; err != nil {
		t.Fatalf("插入评论失败: %v", err)
	}

	_, _, err = newCommentTestService().List(ctx, postID, 1, 10)
	if !errors.Is(err, repository.ErrUserNotFound) {
		t.Fatalf("err = %v, want ErrUserNotFound", err)
	}
}

func TestComment_ListOrdersByCreatedAtThenID(t *testing.T) {
	reset(t)
	ctx := t.Context()
	userRepo, users, posts := newPostTestServices(t)
	authorID := registerUser(t, users, userRepo, "author01", "password1")
	communityID := seedCommunity(t, "Go")
	postID, err := posts.Create(ctx, "排序测试", "正文", authorID, communityID)
	if err != nil {
		t.Fatalf("发帖失败: %v", err)
	}

	// 前两条创建时间相同，翻页时必须按 ID 稳定排序，不能重复或遗漏。
	same := time.Now().Truncate(time.Millisecond)
	var ids []uint
	for _, createdAt := range []time.Time{same, same, same.Add(-time.Minute)} {
		comment := &model.Comment{PostID: postID, AuthorID: authorID, Content: "评论"}
		comment.CreatedAt = createdAt
		if err := testDB.Create(comment).Error; err != nil {
			t.Fatalf("插入评论失败: %v", err)
		}
		ids = append(ids, comment.ID)
	}
	want := []uint{ids[2], ids[0], ids[1]}

	comments := newCommentTestService()
	var got []uint
	for page := 1; page <= 2; page++ {
		items, total, err := comments.List(ctx, postID, page, 2)
		if err != nil {
			t.Fatalf("查询第 %d 页失败: %v", page, err)
		}
		if total != 3 {
			t.Fatalf("第 %d 页 total = %d, want 3", page, total)
		}
		for _, item := range items {
			if item.AuthorUsername != "author01" {
				t.Fatalf("评论 %d 作者 = %q, want author01", item.ID, item.AuthorUsername)
			}
			got = append(got, item.ID)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("评论顺序 = %v, want %v", got, want)
	}
}
