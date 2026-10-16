package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jasper0507/ripple/internal/model"
	"github.com/jasper0507/ripple/internal/repository"
	"github.com/jasper0507/ripple/internal/store"
)

type OutboxWorker struct {
	outboxRepo *repository.OutboxRepository
	postRepo   *repository.PostRepository
	postStore  *store.PostStore
}

func NewOutboxWorker(
	outboxRepo *repository.OutboxRepository,
	postRepo *repository.PostRepository,
	postStore *store.PostStore,
) *OutboxWorker {
	return &OutboxWorker{
		outboxRepo: outboxRepo,
		postRepo:   postRepo,
		postStore:  postStore,
	}
}

// postTask 表示一个待处理的帖子任务
type postTask struct {
	PostID        uint
	NeedIndexSync bool
	VoteUserIDs   map[string]struct{}
	EventIDs      []uint64
}

const (
	outboxBatchSize = 256
	pollInterval    = 500 * time.Millisecond
)

// Run 启动 Outbox Worker
func (w *OutboxWorker) Run(ctx context.Context) error {
	// 持续处理 Outbox，直到 Worker 收到退出信号
	for ctx.Err() == nil {
		n, err := w.processBatch(ctx)
		// 检查服务是否退出
		if ctx.Err() != nil {
			return nil
		}

		if err != nil {
			slog.Error("处理 Outbox 失败", "error", err)
		}

		if err == nil && n > 0 {
			continue
		}

		// 没有待处理事件或本次处理失败时，等待一段时间后重试
		timer := time.NewTimer(pollInterval)

		select {
		case <-ctx.Done():
			timer.Stop()
			return nil

		case <-timer.C:
		}
	}

	return nil
}

// processBatch 处理一批到期的 Outbox 事件
func (w *OutboxWorker) processBatch(
	ctx context.Context,
) (int, error) {
	// 1. 查询当前到期的 Outbox 事件
	events, err := w.outboxRepo.FindDue(
		ctx,
		outboxBatchSize,
	)
	if err != nil {
		return 0, err
	}

	if len(events) == 0 {
		return 0, nil
	}

	// 2. 按帖子合并 Outbox 事件
	// 非法事件单独退避重试，不阻塞同批其他事件，也不让 Worker 退出
	tasks, invalid := coalesce(events)
	for id, cause := range invalid {
		slog.Error("Outbox 事件非法，稍后重试", "event_id", id, "error", cause)

		if err := w.outboxRepo.UpdateRetryByIDs(
			ctx, []uint64{id}, cause,
		); err != nil {
			return len(events), err
		}
	}
	if len(tasks) == 0 {
		return len(events), nil
	}

	// 3. 批量同步帖子投影
	failed, err := w.syncTasks(ctx, tasks)
	if ctx.Err() != nil {
		return len(events), nil
	}
	if err != nil {
		return len(events), err
	}

	// 4. 失败事件重试，成功事件批量确认
	acked := make([]uint64, 0, len(events))

	for _, task := range tasks {
		if err := failed[task.PostID]; err != nil {
			if retryErr := w.outboxRepo.UpdateRetryByIDs(
				ctx, task.EventIDs, err,
			); retryErr != nil {
				return len(events), retryErr
			}
			continue
		}

		acked = append(acked, task.EventIDs...)
	}

	// 5. 删除已经成功处理的事件
	if err := w.outboxRepo.DeleteByIDs(ctx, acked); err != nil {
		return len(events), err
	}

	return len(events), nil
}

// syncTasks 批量读取 MySQL 状态，并同步 Redis 投影
func (w *OutboxWorker) syncTasks(
	ctx context.Context,
	tasks []*postTask,
) (map[uint]error, error) {
	// 收集帖子 ID，并建立 ID 到任务下标的映射
	// MySQL 查询结果不保证顺序，后续通过索引将数据放回对应的同步任务
	ids := make([]uint, len(tasks))
	taskIndex := make(map[uint]int, len(tasks))

	for i, task := range tasks {
		ids[i] = task.PostID
		taskIndex[task.PostID] = i
	}

	// 批量读取帖子当前状态，包括已软删除的帖子
	posts, err := w.postRepo.FindByIDsIncludingDeleted(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(posts) != len(tasks) {
		found := make(map[uint]struct{}, len(posts))
		for _, post := range posts {
			found[post.ID] = struct{}{}
		}

		var missing []uint
		for _, id := range ids {
			if _, ok := found[id]; !ok {
				missing = append(missing, id)
			}
		}

		return nil, fmt.Errorf("Outbox 引用的帖子不存在: post_ids=%v", missing)
	}

	// 按原任务顺序组装 Redis 同步数据
	// 同时收集需要查询的用户投票，避免逐帖访问 MySQL
	syncs := make([]store.PostSync, len(tasks))
	var voteKeys []repository.VoteKey

	for _, post := range posts {
		i := taskIndex[post.ID]
		task := tasks[i]

		syncs[i] = store.PostSync{
			PostID:        post.ID,
			CommunityID:   post.CommunityID,
			CreatedAt:     post.CreatedAt,
			NeedIndexSync: task.NeedIndexSync,
			Deleted:       post.DeletedAt.Valid,
		}

		// 已删除的帖子只需清理投影；没有投票事件则无需查询投票
		if post.DeletedAt.Valid || len(task.VoteUserIDs) == 0 {
			continue
		}

		syncs[i].Votes = make(map[string]int8, len(task.VoteUserIDs))

		// Outbox 只记录涉及的用户，投票方向以 MySQL 当前状态为准
		for userID := range task.VoteUserIDs {
			voteKeys = append(voteKeys, repository.VoteKey{
				PostID: post.ID,
				UserID: userID,
			})
		}
	}

	// 批量读取用户的最终投票方向，填充到对应帖子的同步数据中
	if len(voteKeys) > 0 {
		votes, err := w.postRepo.FindVotes(ctx, voteKeys)
		if err != nil {
			return nil, err
		}

		// 投票与 Outbox 事件在同一事务内写入
		// 用户 ID 已在 coalesce 中去重，每个查询条件应对应一条投票记录
		if len(votes) != len(voteKeys) {
			return nil, errors.New("Outbox 引用的投票数据不完整")
		}

		for _, vote := range votes {
			i := taskIndex[vote.PostID]
			syncs[i].Votes[vote.UserID] = vote.Direction
		}
	}

	return w.postStore.SyncPosts(ctx, syncs), nil
}

// coalesce 将 Outbox 事件按帖子合并为同步任务，并返回非法事件及其原因
func coalesce(
	events []model.OutboxEvent,
) ([]*postTask, map[uint64]error) {
	tasks := make([]*postTask, 0)
	taskByPostID := make(map[uint]*postTask)
	invalid := make(map[uint64]error)

	// 1. 定义帖子任务获取函数，同一帖子复用已有任务
	getTask := func(postID uint) *postTask {
		task, exists := taskByPostID[postID]
		if exists {
			return task
		}

		task = &postTask{
			PostID:      postID,
			VoteUserIDs: make(map[string]struct{}),
		}

		taskByPostID[postID] = task
		tasks = append(tasks, task)

		return task
	}

	// 2. 遍历并合并 Outbox 事件
	for _, event := range events {
		switch event.EventType {
		case model.EventPostIndexSync:
			task := getTask(event.AggregateID)

			// 同一帖子只需要同步一次索引
			task.NeedIndexSync = true
			task.EventIDs = append(task.EventIDs, event.ID)

		case model.EventPostVoteSync:
			if event.Payload == nil {
				invalid[event.ID] = errors.New("投票事件缺少 payload")
				continue
			}

			var payload model.PostVotePayload

			if err := json.Unmarshal(
				[]byte(*event.Payload),
				&payload,
			); err != nil {
				invalid[event.ID] = fmt.Errorf("解析投票事件 payload 失败: %w", err)
				continue
			}

			if payload.UserID == "" {
				invalid[event.ID] = errors.New("投票事件缺少 userID")
				continue
			}

			task := getTask(event.AggregateID)

			// 同一用户只需要同步最终投票状态
			task.VoteUserIDs[payload.UserID] = struct{}{}
			task.EventIDs = append(task.EventIDs, event.ID)

		default:
			invalid[event.ID] = fmt.Errorf("未知事件类型 %q", event.EventType)
		}
	}

	return tasks, invalid
}
