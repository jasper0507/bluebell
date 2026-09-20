package repository

import (
	"context"
	"errors"
	"fmt"
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

	// 使用 ZSet 保存 postID -> vote score，方便后续按分数排序
	postVoteScoreKey = "bluebell:post:scores"

	voteResultOK     = 0
	voteResultClosed = 1
)

var ErrVoteClosed = errors.New("帖子投票已结束")

// voteScript 原子更新用户投票状态和帖子净投票分数
//
// KEYS[1]: 当前帖子的用户投票 Hash
// KEYS[2]: 所有帖子的投票分数 ZSet
//
// ARGV[1]: userID
// ARGV[2]: direction，1: 赞成，0: 取消，-1: 反对
// ARGV[3]: postID
// ARGV[4]: 投票截止时间 Unix 时间戳
var voteScript = redis.NewScript(`
local RESULT_OK = 0
local RESULT_CLOSED = 1

-- 获取当前时间
local now = redis.call("TIME")

-- 检验投票是否截止
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

-- 获取用户当前的投票状态，并比较新旧状态
local new = tonumber(ARGV[2])

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
redis.call("ZINCRBY", KEYS[2], delta, ARGV[3])

-- 设置投票明细的生命周期
redis.call("EXPIREAT", KEYS[1], ARGV[4])

-- 返回结果
return RESULT_OK
`)

// Vote 更新用户对帖子的投票状态
func (r *VoteRepository) Vote(
	ctx context.Context,
	postID uint,
	userID string,
	direction int8,
	expiresAt time.Time,
) error {
	// 1. 构建 votesKey
	postIDStr := strconv.FormatUint(uint64(postID), 10)
	votesKey := postVotesKeyPrefix + postIDStr

	// 2. 原子执行投票脚本
	result, err := voteScript.Run(
		ctx,
		r.rdb,
		[]string{votesKey, postVoteScoreKey},
		userID,
		direction,
		postID,
		expiresAt.Unix(),
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
