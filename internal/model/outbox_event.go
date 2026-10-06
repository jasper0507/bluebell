package model

import "time"

const (
	EventPostIndexSync = "post.index.sync"
	EventPostVoteSync  = "post.vote.sync"
)

// PostVotePayload 只标识需要同步的用户。
// 投票方向由 Worker 执行时查询主库。
type PostVotePayload struct {
	UserID string `json:"user_id"`
}

// OutboxEvent 保存尚未完成的同步通知。
// 成功后物理删除，因此不使用 gorm.Model。
type OutboxEvent struct {
	ID uint64 `gorm:"primaryKey;autoIncrement;index:idx_outbox_due,priority:2"`

	EventType   string  `gorm:"size:32;not null"`
	AggregateID uint    `gorm:"type:bigint unsigned;not null"`
	Payload     *string `gorm:"type:json"`

	RetryCount uint `gorm:"type:int unsigned;not null;default:0"`

	NextRetryAt time.Time `gorm:"type:datetime(3);not null;default:CURRENT_TIMESTAMP(3);index:idx_outbox_due,priority:1"`
	LastError   string    `gorm:"type:text"`

	CreatedAt time.Time `gorm:"type:datetime(3);not null;autoCreateTime:false;default:CURRENT_TIMESTAMP(3)"`
}
