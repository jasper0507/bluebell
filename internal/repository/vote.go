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

// VoteStats 帖子投票统计
type VoteStats struct {
	UpVotes   int64
	DownVotes int64
}

const (
	// 每个帖子使用一个 Hash 保存 userID -> direction
	postVotesKeyPrefix = "bluebell:post:votes:"

	// 使用 Hash 保存 postID -> 赞成票数量
	postUpVoteCountsKey = "bluebell:post:up_vote_counts"

	// 使用 Hash 保存 postID -> 反对票数量
	postDownVoteCountsKey = "bluebell:post:down_vote_counts"

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

// voteScript 原子更新用户投票状态、投票统计、净投票分数和 Hot Score
//
// KEYS[1]: 当前帖子的用户投票 Hash
// KEYS[2]: Vote Score ZSet
// KEYS[3]: Hot Score ZSet
// KEYS[4]: Up Vote Count Hash
// KEYS[5]: Down Vote Count Hash
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

-- 投票统计必须已初始化
if redis.call("HEXISTS", KEYS[4], ARGV[3]) == 0
	or redis.call("HEXISTS", KEYS[5], ARGV[3]) == 0 then
	return redis.error_reply("post vote stats not initialized")
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

-- 根据状态变化计算赞成票和反对票增量
local upDelta = 0
local downDelta = 0

if old == 1 then
	upDelta = upDelta - 1
elseif old == -1 then
	downDelta = downDelta - 1
end

if new == 1 then
	upDelta = upDelta + 1
elseif new == -1 then
	downDelta = downDelta + 1
end

-- 更新用户投票状态
if new == 0 then
	redis.call("HDEL", KEYS[1], ARGV[1])
else
	redis.call("HSET", KEYS[1], ARGV[1], new)
end

-- 更新赞成票和反对票数量
if upDelta ~= 0 then
	redis.call("HINCRBY", KEYS[4], ARGV[3], upDelta)
end

if downDelta ~= 0 then
	redis.call("HINCRBY", KEYS[5], ARGV[3], downDelta)
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

-- 用户投票明细只保留到投票截止时间
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

// InitPost 初始化帖子的投票统计和排序索引
func (r *VoteRepository) InitPost(
	ctx context.Context,
	postID uint,
	createdAt time.Time,
) error {
	postIDStr := strconv.FormatUint(uint64(postID), 10)

	_, err := r.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// 1. 初始化净投票分
		pipe.ZAdd(ctx, postVoteScoreKey, redis.Z{
			Score:  0,
			Member: postIDStr,
		})

		// 2. 初始化赞成票和反对票数量
		pipe.HSet(ctx, postUpVoteCountsKey, postIDStr, 0)
		pipe.HSet(ctx, postDownVoteCountsKey, postIDStr, 0)

		// 3. 建立发布时间排序索引
		pipe.ZAdd(ctx, postTimeScoreKey, redis.Z{
			Score:  float64(createdAt.UnixMilli()),
			Member: postIDStr,
		})

		// 4. 建立热门排序索引
		pipe.ZAdd(ctx, postHotScoreKey, redis.Z{
			Score:  calculateHotScore(0, createdAt),
			Member: postIDStr,
		})

		return nil
	})

	if err != nil {
		return fmt.Errorf("初始化帖子 Redis 数据失败: %w", err)
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

// FindVoteStatsByPostIDs 批量查询帖子的投票统计
func (r *VoteRepository) FindVoteStatsByPostIDs(
	ctx context.Context,
	postIDs []uint,
) (map[uint]VoteStats, error) {
	// 1. 帖子ID转换为 Redis Hash field
	fields := make([]string, 0, len(postIDs))
	for _, id := range postIDs {
		fields = append(fields, strconv.FormatUint(uint64(id), 10))
	}

	// 2. 批量查询赞成与反对票数量
	var upVotesCmd *redis.SliceCmd
	var downVotesCmd *redis.SliceCmd

	_, err := r.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		upVotesCmd = pipe.HMGet(ctx, postUpVoteCountsKey, fields...)
		downVotesCmd = pipe.HMGet(ctx, postDownVoteCountsKey, fields...)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("查询帖子投票统计失败: %w", err)
	}

	upVotesValues := upVotesCmd.Val()
	downVotesValues := downVotesCmd.Val()

	// 3. 按帖子ID构建投票结果
	data := make(map[uint]VoteStats, len(postIDs))

	for i, id := range postIDs {
		upVotes, err := parseVoteCount(upVotesValues[i])
		if err != nil {
			return nil, fmt.Errorf(
				"解析帖子 %d 赞成票统计失败: %w",
				id,
				err,
			)
		}

		downVotes, err := parseVoteCount(downVotesValues[i])
		if err != nil {
			return nil, fmt.Errorf(
				"解析帖子 %d 反对票统计失败: %w",
				id,
				err,
			)
		}

		data[uint(id)] = VoteStats{
			UpVotes:   upVotes,
			DownVotes: downVotes,
		}
	}

	return data, nil
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
			postUpVoteCountsKey,
			postDownVoteCountsKey,
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
