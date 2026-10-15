package repository

import (
	"context"
	"fmt"

	"github.com/jasper0507/bluebell/internal/model"
	"gorm.io/gorm"
)

type OutboxRepository struct {
	db *gorm.DB
}

func NewOutboxRepository(db *gorm.DB) *OutboxRepository {
	return &OutboxRepository{db: db}
}

// appendPostEvent 必须使用业务事务传入的 tx。
func appendPostEvent(
	ctx context.Context,
	tx *gorm.DB,
	eventType string,
	postID uint,
	payload *string,
) error {
	event := &model.OutboxEvent{
		EventType:   eventType,
		AggregateID: postID,
		Payload:     payload,
	}

	if err := gorm.G[model.OutboxEvent](tx).Create(ctx, event); err != nil {
		return fmt.Errorf("写入帖子同步通知失败: %w", err)
	}

	return nil
}

// FindDue 查询已经到期的通知
func (r *OutboxRepository) FindDue(
	ctx context.Context,
	limit int,
) ([]model.OutboxEvent, error) {
	events, err := gorm.G[model.OutboxEvent](r.db).
		Where("next_retry_at <= CURRENT_TIMESTAMP(3)").
		Order("next_retry_at ASC, id ASC").
		Limit(limit).
		Find(ctx)
	if err != nil {
		return nil, fmt.Errorf("查询到期通知失败: %w", err)
	}

	return events, nil
}

// DeleteByIDs 删除已经成功处理的 Outbox 事件
func (r *OutboxRepository) DeleteByIDs(
	ctx context.Context,
	ids []uint64,
) error {
	if len(ids) == 0 {
		return nil
	}

	// 强制使用主键索引，避免优化器选择全表扫描，
	// 导致扫描无关记录并产生锁等待。
	err := gorm.G[model.OutboxEvent](r.db).Exec(
		ctx,
		"DELETE /*+ INDEX(outbox_events PRIMARY) */ FROM outbox_events WHERE id IN ?",
		ids,
	)
	if err != nil {
		return fmt.Errorf("确认同步通知失败: %w", err)
	}

	return nil
}

// UpdateRetryByIDs 更新指定事件的重试信息和错误信息
// 间隔依次为 1、2、4、8、16、32、60 秒，之后保持 60 秒
func (r *OutboxRepository) UpdateRetryByIDs(
	ctx context.Context,
	ids []uint64,
	cause error,
) error {
	if len(ids) == 0 {
		return nil
	}

	err := gorm.G[model.OutboxEvent](r.db).Exec(ctx, `
		UPDATE outbox_events
		SET
			next_retry_at = TIMESTAMPADD(
				SECOND,
				LEAST(60, POW(2, LEAST(retry_count, 6))),
				CURRENT_TIMESTAMP(3)
			),
			retry_count = retry_count + 1,
			last_error = ?
		WHERE id IN ?
	`, cause.Error(), ids)
	if err != nil {
		return fmt.Errorf("安排通知重试失败: %w", err)
	}

	return nil
}
