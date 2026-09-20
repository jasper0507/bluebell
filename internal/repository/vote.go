package repository

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type VoteRepository struct {
	rdb *redis.Client
}

func NewVoteRepository(rdb *redis.Client) *VoteRepository {
	return &VoteRepository{
		rdb: rdb,
	}
}

const (
	// 每个帖子使用一个 Hash 保存 userID -> direction
	postVotesKeyPrefix = "bluebell:post:votes:"

	// 使用 ZSet 保存 postID -> 净投票分数
	postVoteScoreKey = "bluebell:post:vote_scores"

	// 使用 ZSet 保存 postID -> 发布时间
	postTimeScoreKey = "bluebell:post:time_scores"

	// 使用 ZSet 保存 postID -> Reddit Hot Score
	postHotScoreKey = "bluebell:post:hot_scores"

	// Hot Ranking 时间基准：2026-01-01 00:00:00 UTC
	hotEpoch int64 = 1767225600

	// 时间衰减系数：45000 秒 = 12.5 小时
	hotGravity float64 = 45000

	voteResultClosed = 1
)

var ErrVoteClosed = errors.New("帖子投票已结束")

// voteScript 原子更新用户投票状态、净投票分数和 Hot Score
//
// KEYS[1]: 当前帖子的用户投票 Hash
// KEYS[2]: Vote Score ZSet
// KEYS[3]: Hot Score ZSet
//
// ARGV[1]: userID
// ARGV[2]: direction，1: 赞成，0: 取消，-1: 反对
// ARGV[3]: postID
// ARGV[4]: 投票截止时间 Unix 时间戳
// ARGV[5]: 帖子创建时间 Unix 时间戳
// ARGV[6]: Hot Epoch
// ARGV[7]: Hot Gravity
var voteScript = redis.NewScript(`
local RESULT_OK = 0
local RESULT_CLOSED = 1

-- 保留 7 位小数
local function round7(value)
	local factor = 10000000

	if value >= 0 then
		return math.floor(value * factor + 0.5) / factor
	end

	return math.ceil(value * factor - 0.5) / factor
end

-- 获取 Redis 服务器当前时间
local now = redis.call("TIME")

-- 检查投票是否截止
if tonumber(now[1]) >= tonumber(ARGV[4]) then
	return RESULT_CLOSED
end

-- 获取用户原来的投票状态，不存在视为未投票
local old = redis.call("HGET", KEYS[1], ARGV[1])

if old == false then
	old = 0
else
	old = tonumber(old)
end

local new = tonumber(ARGV[2])

-- 投票状态没有变化直接返回
if old == new then
	return RESULT_OK
end

-- 更新用户投票状态
if new == 0 then
	redis.call("HDEL", KEYS[1], ARGV[1])
else
	redis.call("HSET", KEYS[1], ARGV[1], new)
end

-- 更新帖子净投票分数
local delta = new - old
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

local seconds = tonumber(ARGV[5]) - tonumber(ARGV[6])
local hotScore = sign * order + seconds / tonumber(ARGV[7])
hotScore = round7(hotScore)

redis.call("ZADD", KEYS[3], hotScore, ARGV[3])

-- 投票明细只保留到投票截止时间
redis.call("EXPIREAT", KEYS[1], ARGV[4])

return RESULT_OK
`)

// calculateHotScore 计算帖子的热门分数
func calculateHotScore(voteScore int64, createdAt time.Time) float64 {
	// 1. 计算投票贡献分
	score := float64(voteScore)
	// 对数增长票数分
	order := math.Log10(
		math.Max(math.Abs(score), 1),
	)
	signedOrder := math.Copysign(order, score)

	// 2. 计算时间贡献分
	seconds := float64(createdAt.Unix() - hotEpoch)

	// 3. 计算总分并返回
	hotScore := signedOrder + seconds/hotGravity

	return math.Round(hotScore*1e7) / 1e7
}

// InitPostRanking 初始化帖子的投票分数和排序索引
func (r *VoteRepository) InitPostRanking(
	ctx context.Context,
	postID uint,
	createdAt time.Time,
) error {
	postIDStr := strconv.FormatUint(uint64(postID), 10)

	_, err := r.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		// 1. 设置新帖子初始净投票分为 0
		pipe.ZAdd(ctx, postVoteScoreKey, redis.Z{
			Score:  0,
			Member: postIDStr,
		})

		// 2. 按发布时间建立最新排序索引
		pipe.ZAdd(ctx, postTimeScoreKey, redis.Z{
			Score:  float64(createdAt.UnixMilli()),
			Member: postIDStr,
		})

		// 3. 建立初始热门排序索引
		pipe.ZAdd(ctx, postHotScoreKey, redis.Z{
			Score:  calculateHotScore(0, createdAt),
			Member: postIDStr,
		})

		return nil
	})

	if err != nil {
		return fmt.Errorf("初始化帖子排名失败: %w", err)
	}

	return nil
}

// FindPostIDsByTime 按发布时间倒序分页查询帖子ID
func (r *VoteRepository) FindPostIDsByTime(
	ctx context.Context,
	offset,
	limit int,
) ([]uint, error) {
	return r.findPostIDsByRank(
		ctx,
		postTimeScoreKey,
		offset,
		limit,
	)
}

// FindPostIDsByHot 按 Hot Score 倒序分页查询帖子ID
func (r *VoteRepository) FindPostIDsByHot(
	ctx context.Context,
	offset,
	limit int,
) ([]uint, error) {
	return r.findPostIDsByRank(
		ctx,
		postHotScoreKey,
		offset,
		limit,
	)
}

// findPostIDsByRank 根据指定排行榜分页查询帖子ID
func (r *VoteRepository) findPostIDsByRank(
	ctx context.Context,
	key string,
	offset,
	limit int,
) ([]uint, error) {
	// 1. 设置range范围
	start := int64(offset)
	stop := start + int64(limit) - 1

	// 2. 从 ZSet 里按分数倒序取成员
	members, err := r.rdb.ZRangeArgs(ctx, redis.ZRangeArgs{
		Key:   key,
		Start: start,
		Stop:  stop,
		Rev:   true,
	}).Result()

	if err != nil {
		return nil, fmt.Errorf("查询帖子排名失败: %w", err)
	}

	// 3. 解析成员为帖子ID
	ids := make([]uint, 0, len(members))

	for _, member := range members {
		id, err := strconv.ParseUint(member, 10, strconv.IntSize)
		if err != nil {
			return nil, fmt.Errorf("解析帖子ID失败: %w", err)
		}

		ids = append(ids, uint(id))
	}

	return ids, nil
}

// Vote 更新用户对帖子的投票状态
func (r *VoteRepository) Vote(
	ctx context.Context,
	postID uint,
	userID string,
	direction int8,
	createdAt,
	expiresAt time.Time,
) error {
	// 1. 构建 votesKey
	postIDStr := strconv.FormatUint(uint64(postID), 10)
	votesKey := postVotesKeyPrefix + postIDStr

	// 2. 原子执行投票脚本
	result, err := voteScript.Run(
		ctx,
		r.rdb,
		[]string{
			votesKey,
			postVoteScoreKey,
			postHotScoreKey,
		},
		userID,
		direction,
		postIDStr,
		expiresAt.Unix(),
		createdAt.Unix(),
		hotEpoch,
		hotGravity,
	).Int()

	if err != nil {
		return fmt.Errorf("更新帖子投票失败: %w", err)
	}

	// 3. 投票结束返回
	if result == voteResultClosed {
		return ErrVoteClosed
	}

	return nil
}
