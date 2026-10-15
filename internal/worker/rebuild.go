package worker

import (
	"context"
	"fmt"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/store"
)

// Rebuild 清空 Redis 帖子投影，再按 MySQL 当前数据全量重建
func (w *OutboxWorker) Rebuild(ctx context.Context) (int, error) {
	// 1. 清空旧投影
	if err := w.postStore.DeleteAll(ctx); err != nil {
		return 0, err
	}

	total := 0

	// 2. 按主键分批读取未删除帖子，逐批写入 Redis
	err := w.postRepo.FindAllInBatches(ctx, outboxBatchSize, func(posts []model.Post) error {
		// 收集帖子 ID，并建立 ID 到同步数据下标的映射
		ids := make([]uint, len(posts))
		index := make(map[uint]int, len(posts))
		syncs := make([]store.PostSync, len(posts))

		for i, post := range posts {
			ids[i] = post.ID
			index[post.ID] = i

			syncs[i] = store.PostSync{
				PostID:        post.ID,
				CommunityID:   post.CommunityID,
				CreatedAt:     post.CreatedAt,
				NeedIndexSync: true,
				Votes:         make(map[string]int8),
			}
		}

		// 批量读取本批帖子的全部有效投票，避免逐帖访问 MySQL
		votes, err := w.postRepo.FindVotesByPostIDs(ctx, ids)
		if err != nil {
			return err
		}

		for _, vote := range votes {
			syncs[index[vote.PostID]].Votes[vote.UserID] = vote.Direction
		}

		for postID, err := range w.postStore.SyncPosts(ctx, syncs) {
			return fmt.Errorf("重建帖子 %d 投影失败: %w", postID, err)
		}

		total += len(posts)

		return nil
	})

	return total, err
}
