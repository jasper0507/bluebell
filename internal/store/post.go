package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type PostStore struct {
	rdb *redis.Client
}

func NewPostStore(rdb *redis.Client) *PostStore {
	return &PostStore{
		rdb: rdb,
	}
}

const (
	// 每个帖子使用一个 Hash 保存 userID -> direction
	postVotesKeyPrefix = "bluebell:post:votes:"

	// 使用 ZSet 保存 postID -> 净投票分数
	postVoteScoreKey = "bluebell:post:vote_scores"

	// 帖子排行榜 Key 前缀
	postRankKeyPrefix = "bluebell:post:rank:"

	postRankTime = "time"
	postRankHot  = "hot"

	// Hot Ranking 时间基准：2026-01-01 00:00:00 UTC
	hotEpoch int64 = 1767225600

	// 时间衰减系数：45000 秒 = 12.5 小时
	hotGravity float64 = 45000
)

var ErrPostProjectionNotInitialized = errors.New("帖子投影尚未初始化")

// initPostScript 原子初始化帖子投影，重复执行时不会覆盖已有数据
//
// KEYS[1]: Vote Score ZSet
// KEYS[2]: 全站 Time 排行榜
// KEYS[3]: 社区 Time 排行榜
// KEYS[4]: 全站 Hot 排行榜
// KEYS[5]: 社区 Hot 排行榜
//
// ARGV[1]: postID
// ARGV[2]: 帖子创建时间 Unix 毫秒时间戳
// ARGV[3]: 初始 Hot Score
//
// 返回值
// 0: 初始化成功或帖子投影已完整存在
//
// 错误
// post projection incomplete: 帖子投影部分缺失
var initPostScript = redis.NewScript(`
local postID = ARGV[1]

-- 检查当前帖子初始化状态
local present = 0

for i = 1, 5 do
	if redis.call("ZSCORE", KEYS[i], postID) ~= false then
		present = present + 1
	end
end

if present ~= 0 and present ~= 5 then
	return redis.error_reply("post projection incomplete")
end

if present == 5 then
	return 0
end

-- 初始化净投票分和排序索引
redis.call("ZADD", KEYS[1], "NX", 0, postID)
redis.call("ZADD", KEYS[2], "NX", ARGV[2], postID)
redis.call("ZADD", KEYS[3], "NX", ARGV[2], postID)
redis.call("ZADD", KEYS[4], "NX", ARGV[3], postID)
redis.call("ZADD", KEYS[5], "NX", ARGV[3], postID)

return 0
`)

// applyVoteScript 原子应用用户投票状态，更新净投票分数和热度分数。
//
// KEYS[1]: 当前帖子的用户投票 Hash
// KEYS[2]: Vote Score ZSet
// KEYS[3]: 全站 Hot 排行榜 ZSet
// KEYS[4]: 社区 Hot 排行榜 ZSet
//
// ARGV[1]: userID
// ARGV[2]: direction，1: 赞成，0: 取消，-1: 反对
// ARGV[3]: postID
// ARGV[4]: 帖子创建时间 Unix 时间戳
// ARGV[5]: Hot Epoch
// ARGV[6]: Hot Gravity
//
// 返回值
// 0：投票状态已应用或无需变更
// 1：帖子投影尚未初始化
var applyVoteScript = redis.NewScript(`
-- 帖子投影尚未初始化，等待重试
if redis.call("ZSCORE", KEYS[2], ARGV[3]) == false then
return 1
end
-- 获取 Redis 已应用的用户投票状态，不存在视为未投票
local old = redis.call("HGET", KEYS[1], ARGV[1])

if old == false then
	old = 0
else
	old = tonumber(old)
end

local new = tonumber(ARGV[2])

-- 投票状态没有变化直接返回
if old == new then
	return 0
end

-- 计算投票状态变化量
local delta = new - old

-- 更新用户投票状态
if new == 0 then
	redis.call("HDEL", KEYS[1], ARGV[1])
else
	redis.call("HSET", KEYS[1], ARGV[1], new)
end

-- 更新净投票分数
local voteScore = tonumber(
	redis.call("ZINCRBY", KEYS[2], delta, ARGV[3])
)

-- 根据最新净投票分重新计算 Hot Score
local order = math.log10(math.max(math.abs(voteScore), 1))
local sign = 0

if voteScore > 0 then
	sign = 1
elseif voteScore < 0 then
	sign = -1
end

local seconds = tonumber(ARGV[4]) - tonumber(ARGV[5])
local hotScore = sign * order + seconds / tonumber(ARGV[6])

-- 同步更新全站和社区 Hot 排行榜
redis.call("ZADD", KEYS[3], hotScore, ARGV[3])
redis.call("ZADD", KEYS[4], hotScore, ARGV[3])

return 0
`)

// InitPost 初始化帖子的投票统计和排序索引
func (s *PostStore) InitPost(
	ctx context.Context,
	postID,
	communityID uint,
	createdAt time.Time,
) error {
	postIDStr := strconv.FormatUint(uint64(postID), 10)
	initialHotScore := float64(createdAt.Unix()-hotEpoch) / hotGravity

	err := initPostScript.Run(
		ctx,
		s.rdb,
		[]string{
			postVoteScoreKey,
			postRankKey(nil, postRankTime),
			postRankKey(&communityID, postRankTime),
			postRankKey(nil, postRankHot),
			postRankKey(&communityID, postRankHot),
		},
		postIDStr,
		createdAt.UnixMilli(),
		initialHotScore,
	).Err()

	if err != nil {
		return fmt.Errorf("初始化帖子投影失败: %w", err)
	}

	return nil
}

// DeletePostData 删除帖子的投票数据和排序索引
func (s *PostStore) DeletePostData(
	ctx context.Context,
	postID,
	communityID uint,
) error {
	postIDStr := strconv.FormatUint(uint64(postID), 10)

	_, err := s.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// 1. 删除用户投票明细
		pipe.Del(
			ctx,
			postVotesKeyPrefix+postIDStr,
		)

		// 2. 删除净投票分
		pipe.ZRem(
			ctx,
			postVoteScoreKey,
			postIDStr,
		)

		// 3. 删除全站排行榜索引
		pipe.ZRem(
			ctx,
			postRankKey(nil, postRankTime),
			postIDStr,
		)
		pipe.ZRem(
			ctx,
			postRankKey(nil, postRankHot),
			postIDStr,
		)

		// 4. 删除社区排行榜索引
		pipe.ZRem(
			ctx,
			postRankKey(&communityID, postRankTime),
			postIDStr,
		)
		pipe.ZRem(
			ctx,
			postRankKey(&communityID, postRankHot),
			postIDStr,
		)

		return nil
	})

	if err != nil {
		return fmt.Errorf("删除帖子 Redis 数据失败: %w", err)
	}

	return nil
}

// ApplyVote 将 MySQL 中的用户投票状态应用到 Redis 投影
func (s *PostStore) ApplyVote(
	ctx context.Context,
	postID,
	communityID uint,
	userID string,
	direction int8,
	createdAt time.Time,
) error {
	postIDStr := strconv.FormatUint(uint64(postID), 10)

	result, err := applyVoteScript.Run(
		ctx,
		s.rdb,
		[]string{
			postVotesKeyPrefix + postIDStr,
			postVoteScoreKey,
			postRankKey(nil, postRankHot),
			postRankKey(&communityID, postRankHot),
		},
		userID,
		direction,
		postIDStr,
		createdAt.Unix(),
		hotEpoch,
		hotGravity,
	).Int64()

	if err != nil {
		return fmt.Errorf("应用帖子投票投影失败: %w", err)
	}

	if result == 1 {
		return ErrPostProjectionNotInitialized
	}

	return nil
}

// FindPostIDs 按指定范围和排序方式分页查询帖子 ID
func (s *PostStore) FindPostIDs(
	ctx context.Context,
	communityID *uint,
	order string,
	offset,
	limit int,
) ([]uint, int64, error) {
	key := postRankKey(
		communityID,
		order,
	)

	start := int64(offset)
	stop := start + int64(limit) - 1

	var (
		membersCmd *redis.StringSliceCmd
		totalCmd   *redis.IntCmd
	)

	_, err := s.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		// 1. 查询当前页帖子 ID
		membersCmd = pipe.ZRangeArgs(
			ctx,
			redis.ZRangeArgs{
				Key:   key,
				Start: start,
				Stop:  stop,
				Rev:   true,
			},
		)

		// 2. 查询帖子总数
		totalCmd = pipe.ZCard(
			ctx,
			key,
		)

		return nil
	})

	if err != nil {
		return nil, 0, fmt.Errorf("查询帖子排名失败: %w", err)
	}

	// 3. 解析帖子 ID
	members := membersCmd.Val()
	ids := make([]uint, 0, len(members))

	for _, member := range members {
		id, err := strconv.ParseUint(
			member,
			10,
			strconv.IntSize,
		)
		if err != nil {
			return nil, 0, fmt.Errorf(
				"解析帖子ID失败: %w",
				err,
			)
		}

		ids = append(ids, uint(id))
	}

	return ids, totalCmd.Val(), nil
}

// postRankKey 返回指定范围和排序方式的帖子排行榜 Key
func postRankKey(
	communityID *uint,
	order string,
) string {
	if communityID == nil {
		return postRankKeyPrefix + "global:" + order
	}

	return postRankKeyPrefix +
		"community:" +
		strconv.FormatUint(uint64(*communityID), 10) +
		":" +
		order
}

// parseVoteCount 解析 Redis 中的投票统计值
func parseVoteCount(value any) (int64, error) {
	if value == nil {
		return 0, errors.New("投票统计不存在")
	}

	raw, ok := value.(string)
	if !ok {
		return 0, fmt.Errorf("投票统计类型错误: %T", value)
	}

	count, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析投票统计失败: %w", err)
	}

	return count, nil
}
