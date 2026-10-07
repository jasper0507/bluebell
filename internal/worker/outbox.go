package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jasper0507/bluebell/internal/model"
	"github.com/jasper0507/bluebell/internal/repository"
	"github.com/jasper0507/bluebell/internal/store"
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
	taskTimeout     = 30 * time.Second
)

var ErrInvalidOutboxEvent = errors.New("非法 Outbox 事件")

// Run 启动 Outbox Worker
func (w *OutboxWorker) Run(ctx context.Context) error {
	// 持续处理 Outbox，直到 Worker 收到退出信号
	for ctx.Err() == nil {
		n, err := w.processBatch(ctx)
		// 检查服务是否退出
		if ctx.Err() != nil {
			return nil
		}

		if errors.Is(err, ErrInvalidOutboxEvent) {
			return err
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
	tasks, err := coalesce(events)
	if err != nil {
		return len(events), err
	}

	acked := make([]uint64, 0, len(events))

	// 3. 依次处理帖子同步任务
	for _, task := range tasks {
		// 如果 Worker 退出，不再开始新的任务
		if ctx.Err() != nil {
			return len(events), nil
		}

		// 创建当前任务上下文，设置超时
		taskCtx, cancel := context.WithTimeout(
			ctx,
			taskTimeout,
		)

		// 执行任务
		err := w.processTask(taskCtx, task)
		// 任务执行完毕后取消上下文
		cancel()

		// 如果 Worker 退出，保留未确认事件，下次启动重新处理
		if ctx.Err() != nil {
			return len(events), nil
		}

		// 处理失败时延迟重试
		if err != nil {
			if retryErr := w.outboxRepo.UpdateRetryByIDs(
				ctx,
				task.EventIDs,
				err,
			); retryErr != nil {
				return len(events), retryErr
			}

			continue
		}

		acked = append(acked, task.EventIDs...)
	}

	// 4. 删除已经成功处理的 Outbox 事件
	if err := w.outboxRepo.DeleteByIDs(ctx, acked); err != nil {
		return len(events), err
	}

	return len(events), nil
}

// processTask 处理单个帖子同步任务
func (w *OutboxWorker) processTask(
	ctx context.Context,
	task *postTask,
) error {
	// 1. 查询帖子当前状态
	post, err := w.postRepo.FindByIDIncludingDeleted(
		ctx,
		task.PostID,
	)
	if err != nil {
		return err
	}

	// 2. 帖子已删除则清理 Redis 投影
	if post.DeletedAt.Valid {
		return w.postStore.DeletePostData(
			ctx,
			post.ID,
			post.CommunityID,
		)
	}

	// 3. 同步帖子索引投影
	if task.NeedIndexSync {
		if err := w.postStore.InitPost(
			ctx,
			post.ID,
			post.CommunityID,
			post.CreatedAt,
		); err != nil {
			return err
		}
	}

	if len(task.VoteUserIDs) == 0 {
		return nil
	}

	// 4. 收集需要同步投票状态的用户 ID
	userIDs := make([]string, 0, len(task.VoteUserIDs))

	for userID := range task.VoteUserIDs {
		userIDs = append(userIDs, userID)
	}

	// 5. 从 MySQL 批量查询用户当前投票状态
	votes, err := w.postRepo.FindVotes(
		ctx,
		post.ID,
		userIDs,
	)
	if err != nil {
		return err
	}

	// 6. 将当前投票状态同步到 Redis 投影
	for _, vote := range votes {
		if err := w.postStore.ApplyVote(
			ctx,
			post.ID,
			post.CommunityID,
			vote.UserID,
			vote.Direction,
			post.CreatedAt,
		); err != nil {
			return err
		}
	}

	return nil
}

// coalesce 将 Outbox 事件按帖子合并为同步任务
func coalesce(
	events []model.OutboxEvent,
) ([]*postTask, error) {
	tasks := make([]*postTask, 0)
	taskByPostID := make(map[uint]*postTask)

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
				return nil, fmt.Errorf(
					"%w: 投票通知 %d 缺少 payload",
					ErrInvalidOutboxEvent,
					event.ID,
				)
			}

			var payload model.PostVotePayload

			if err := json.Unmarshal(
				[]byte(*event.Payload),
				&payload,
			); err != nil {
				return nil, fmt.Errorf(
					"%w: 解析投票通知 %d payload 失败: %v",
					ErrInvalidOutboxEvent,
					event.ID,
					err,
				)
			}

			if payload.UserID == "" {
				return nil, fmt.Errorf(
					"%w: 投票通知 %d 缺少 userID",
					ErrInvalidOutboxEvent,
					event.ID,
				)
			}

			task := getTask(event.AggregateID)

			// 同一用户只需要同步最终投票状态
			task.VoteUserIDs[payload.UserID] = struct{}{}
			task.EventIDs = append(task.EventIDs, event.ID)

		default:
			return nil, fmt.Errorf(
				"%w: 未知通知类型 %q，eventID=%d",
				ErrInvalidOutboxEvent,
				event.EventType,
				event.ID,
			)
		}
	}

	return tasks, nil
}
