package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jasper0507/bluebell/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostRepository struct {
	db *gorm.DB
}

func NewPostRepository(db *gorm.DB) *PostRepository {
	return &PostRepository{
		db: db,
	}
}

type VoteKey struct {
	PostID uint
	UserID string
}

var ErrPostNotFound = errors.New("帖子不存在")

// Create 在同一事务中创建帖子并追加索引同步通知。
func (r *PostRepository) Create(
	ctx context.Context,
	post *model.Post,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := gorm.G[model.Post](tx).Create(ctx, post); err != nil {
			return fmt.Errorf("插入帖子失败: %w", err)
		}

		return appendPostEvent(
			ctx,
			tx,
			model.EventPostIndexSync,
			post.ID,
			nil,
		)
	})
}

// Delete 软删除帖子并追加索引同步通知
func (r *PostRepository) Delete(
	ctx context.Context,
	id uint,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		rows, err := gorm.G[model.Post](tx).
			Where("id = ?", id).
			Delete(ctx)

		if err != nil {
			return fmt.Errorf("软删除帖子失败: %w", err)
		}

		if rows == 0 {
			return ErrPostNotFound
		}

		return appendPostEvent(
			ctx,
			tx,
			model.EventPostIndexSync,
			id,
			nil,
		)
	})
}

// Vote 保存用户当前投票，并追加投票同步通知。
func (r *PostRepository) Vote(
	ctx context.Context,
	postID uint,
	userID string,
	direction int8,
) error {
	data, err := json.Marshal(model.PostVotePayload{
		UserID: userID,
	})
	if err != nil {
		return fmt.Errorf("编码投票同步通知失败: %w", err)
	}

	payload := string(data)

	return r.db.Transaction(func(tx *gorm.DB) error {
		// 锁住帖子，避免存在性检查后立即被并发删除
		_, err := gorm.G[model.Post](
			tx,
			clause.Locking{Strength: "SHARE"},
		).
			Select("id").
			Where("id = ?", postID).
			First(ctx)

		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPostNotFound
		}
		if err != nil {
			return fmt.Errorf("检查帖子失败: %w", err)
		}

		// 保存投票，存在则更新状态（Upsert 操作）
		err = gorm.G[model.PostVote](tx).Exec(ctx,
			`
			INSERT INTO post_votes (post_id, user_id, direction)
			VALUES (?, ?, ?) AS new_row
			ON DUPLICATE KEY UPDATE direction = new_row.direction
			`,
			postID,
			userID,
			direction,
		)

		if err != nil {
			return fmt.Errorf("保存投票失败: %w", err)
		}

		return appendPostEvent(
			ctx,
			tx,
			model.EventPostVoteSync,
			postID,
			&payload,
		)
	})
}

// CountVotesByPostID 统计帖子的赞成票和反对票
func (r *PostRepository) CountVotesByPostID(
	ctx context.Context,
	postID uint,
) (int64, int64, error) {
	var stats struct {
		UpVotes   int64 `gorm:"column:up_votes"`
		DownVotes int64 `gorm:"column:down_votes"`
	}

	err := gorm.G[model.PostVote](r.db).
		Select(`
			COUNT(CASE WHEN direction = 1  THEN 1 END) AS up_votes,
			COUNT(CASE WHEN direction = -1 THEN 1 END) AS down_votes
		`).
		Where("post_id = ?", postID).
		Scan(ctx, &stats)
	if err != nil {
		return 0, 0, fmt.Errorf("统计帖子投票失败: %w", err)
	}

	return stats.UpVotes, stats.DownVotes, nil
}

// FindByID 根据 ID 查询帖子
func (r *PostRepository) FindByID(ctx context.Context, id uint) (*model.Post, error) {
	post, err := gorm.G[model.Post](r.db).
		Where("id = ?", id).
		First(ctx)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPostNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("根据 ID 查询帖子失败: %w", err)
	}

	return &post, nil
}

// FindByIDs 根据 ID 列表批量查询帖子
func (r *PostRepository) FindByIDs(
	ctx context.Context,
	ids []uint,
) ([]model.Post, error) {
	posts, err := gorm.G[model.Post](r.db).
		Select("id, title, author_id, community_id, created_at").
		Where("id IN ?", ids).
		Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("根据ID列表查询帖子失败: %w", err)
	}

	return posts, nil
}

// FindByIDsIncludingDeleted 批量查询帖子同步状态，包含软删除记录。
func (r *PostRepository) FindByIDsIncludingDeleted(
	ctx context.Context,
	ids []uint,
) ([]model.Post, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var posts []model.Post

	err := r.db.WithContext(ctx).
		Unscoped().
		Select("id, community_id, created_at, deleted_at").
		Where("id IN ?", ids).
		Find(&posts).Error

	if err != nil {
		return nil, fmt.Errorf("批量查询帖子同步状态失败: %w", err)
	}

	return posts, nil
}

// FindVotes 批量查询指定帖子和用户的最终投票状态。
func (r *PostRepository) FindVotes(
	ctx context.Context,
	keys []VoteKey,
) ([]model.PostVote, error) {
	if len(keys) == 0 {
		return nil, nil
	}

	pairs := make([][]any, 0, len(keys))
	for _, key := range keys {
		pairs = append(pairs, []any{key.PostID, key.UserID})
	}

	votes, err := gorm.G[model.PostVote](r.db).
		Where("(post_id, user_id) IN ?", pairs).
		Find(ctx)

	if err != nil {
		return nil, fmt.Errorf("批量查询投票状态失败: %w", err)
	}

	return votes, nil
}

// FindUserVote 查询用户对帖子的当前投票状态
func (r *PostRepository) FindUserVote(
	ctx context.Context,
	postID uint,
	userID string,
) (int8, error) {
	vote, err := gorm.G[model.PostVote](r.db).
		Where("post_id = ? AND user_id = ?", postID, userID).
		First(ctx)

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("查询用户投票状态失败: %w", err)
	}

	return vote.Direction, nil
}
