package store

import (
	"context"
	"errors"
	"fmt"
	"math"
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

	// 帖子排行榜 Key 前缀
	postRankKeyPrefix = "bluebell:post:rank:"

	postRankTime = "time"
	postRankHot  = "hot"

	// Hot Ranking 时间基准：2026-01-01 00:00:00 UTC
	hotEpoch int64 = 1767225600

	// 时间衰减系数：45000 秒 = 12.5 小时
	hotGravity float64 = 45000
)

var ErrProjectionNotInit = errors.New("帖子投影尚未初始化")

// initPostScript 原子初始化帖子投影，重复执行时不会覆盖已有数据
//
// KEYS[1]: Up Vote Count Hash
// KEYS[2]: Down Vote Count Hash
// KEYS[3]: Vote Score ZSet
// KEYS[4]: 全站 Time 排行榜 ZSet
// KEYS[5]: 社区 Time 排行榜 ZSet
// KEYS[6]: 全站 Hot 排行榜 ZSet
// KEYS[7]: 社区 Hot 排行榜 ZSet
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
local post = ARGV[1]
-- 检查当前帖子初始化状态
local present = 0

if redis.call("HEXISTS", KEYS[1], post) == 1 then
	present = present + 1
end

if redis.call("HEXISTS", KEYS[2], post) == 1 then
	present = present + 1
end

for i = 3, 7 do
	if redis.call("ZSCORE", KEYS[i], post) ~= false then
		present = present + 1
	end
end

if present ~= 0 and present ~= 7 then
	return redis.error_reply("post projection incomplete")
end

if present == 7 then
	return 0
end
-- 初始化帖子投票统计和排序索引
redis.call("HSETNX", KEYS[1], post, 0)
redis.call("HSETNX", KEYS[2], post, 0)

redis.call("ZADD", KEYS[3], "NX", 0, post)
redis.call("ZADD", KEYS[4], "NX", ARGV[2], post)
redis.call("ZADD", KEYS[5], "NX", ARGV[2], post)
redis.call("ZADD", KEYS[6], "NX", ARGV[3], post)
redis.call("ZADD", KEYS[7], "NX", ARGV[3], post)

return 0
`)

// applyVoteScript 原子应用用户投票状态，更新投票统计、净投票分数和 Hot Score
//
// KEYS[1]: 当前帖子的用户投票 Hash
// KEYS[2]: Vote Score ZSet
// KEYS[3]: 全站 Hot 排行榜 ZSet
// KEYS[4]: Up Vote Count Hash
// KEYS[5]: Down Vote Count Hash
// KEYS[6]: 社区 Hot 排行榜 ZSet
//
// ARGV[1]: userID
// ARGV[2]: direction，1: 赞成，0: 取消，-1: 反对
// ARGV[3]: postID
// ARGV[4]: 帖子创建时间 Unix 时间戳
// ARGV[5]: Hot Epoch
// ARGV[6]: Hot Gravity
//
// 返回值
// [1]: 投票结果，0 表示成功，1 表示帖子投影尚未初始化
var applyVoteScript = redis.NewScript(`
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

-- 获取当前投票统计，帖子投影必须已初始化
local up = tonumber(redis.call("HGET", KEYS[4], ARGV[3]))
local down = tonumber(redis.call("HGET", KEYS[5], ARGV[3]))

if up == nil or down == nil then
	return 1
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

local newUp = up + upDelta
local newDown = down + downDelta
local voteScore = newUp - newDown

-- 更新用户投票状态
if new == 0 then
	redis.call("HDEL", KEYS[1], ARGV[1])
else
	redis.call("HSET", KEYS[1], ARGV[1], new)
end

-- 更新投票统计和净投票分数
redis.call("HSET", KEYS[4], ARGV[3], newUp)
redis.call("HSET", KEYS[5], ARGV[3], newDown)
redis.call("ZADD", KEYS[2], voteScore, ARGV[3])

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
redis.call("ZADD", KEYS[6], hotScore, ARGV[3])

return 0
`)

// InitPost 初始化帖子的投票统计和排序索引
func (r *PostStore) InitPost(
	ctx context.Context,
	postID,
	communityID uint,
	createdAt time.Time,
) error {
	postIDStr := strconv.FormatUint(uint64(postID), 10)

	err := initPostScript.Run(
		ctx,
		r.rdb,
		[]string{
			postUpVoteCountsKey,
			postDownVoteCountsKey,
			postVoteScoreKey,
			postRankKey(nil, postRankTime),
			postRankKey(&communityID, postRankTime),
			postRankKey(nil, postRankHot),
			postRankKey(&communityID, postRankHot),
		},
		postIDStr,
		createdAt.UnixMilli(),
		calculateHotScore(0, createdAt),
	).Err()

	if err != nil {
		return fmt.Errorf("初始化帖子投影失败: %w", err)
	}

	return nil
}

// DeletePostData 删除帖子的投票数据和排序索引
func (r *PostStore) DeletePostData(
	ctx context.Context,
	postID,
	communityID uint,
) error {
	postIDStr := strconv.FormatUint(uint64(postID), 10)

	_, err := r.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		// 1. 删除用户投票明细
		pipe.Del(
			ctx,
			postVotesKeyPrefix+postIDStr,
		)

		// 2. 删除投票统计
		pipe.HDel(
			ctx,
			postUpVoteCountsKey,
			postIDStr,
		)
		pipe.HDel(
			ctx,
			postDownVoteCountsKey,
			postIDStr,
		)

		// 3. 删除净投票分
		pipe.ZRem(
			ctx,
			postVoteScoreKey,
			postIDStr,
		)

		// 4. 删除全站排行榜索引
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

		// 5. 删除社区排行榜索引
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
func (r *PostStore) ApplyVote(
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
		r.rdb,
		[]string{
			postVotesKeyPrefix + postIDStr,
			postVoteScoreKey,
			postRankKey(nil, postRankHot),
			postUpVoteCountsKey,
			postDownVoteCountsKey,
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
		return ErrProjectionNotInit
	}

	return nil
}

// FindPostIDs 按指定范围和排序方式分页查询帖子ID
func (r *PostStore) FindPostIDs(
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

	_, err := r.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		// 1. 查询当前页帖子ID
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

	// 3. 解析帖子ID
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

// FindVoteStatsByPostIDs 批量查询帖子的投票统计
func (r *PostStore) FindVoteStatsByPostIDs(
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

	_, err := r.rdb.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
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
	return signedOrder + seconds/hotGravity
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
