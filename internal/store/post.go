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

// PostSync 用于同步单个帖子的 Redis 投影
type PostSync struct {
	PostID        uint
	CommunityID   uint
	CreatedAt     time.Time
	NeedIndexSync bool
	Deleted       bool
	Votes         map[string]int8
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

// syncPostScript 原子同步单个帖子的 Redis 投影。
//
// KEYS[1]: 用户投票 Hash
// KEYS[2]: 净投票分 ZSet
// KEYS[3:6]: 全站 Time、社区 Time、全站 Hot、社区 Hot
//
// ARGV[1]: postID
// ARGV[2]: 操作类型：-1 删除，0 投票，1 初始化
// ARGV[3:6]: 创建时间毫秒、创建时间秒、Hot Epoch、Hot Gravity
// ARGV[7...]: userID、direction 成对排列
//
// 返回：0 成功，1 投影未初始化
const syncPostScriptSource = `
local id = ARGV[1]
local op = tonumber(ARGV[2])

-- 删除帖子的所有投影
if op == -1 then
    redis.call("DEL", KEYS[1])

    for i = 2, 6 do
        redis.call("ZREM", KEYS[i], id)
    end

    return 0
end

-- 初始化帖子索引，重复执行不覆盖已有投票
if op == 1 then
    local present = 0

-- 先判断初始化状态
    for i = 2, 6 do
        if redis.call("ZSCORE", KEYS[i], id) ~= false then
            present = present + 1
        end
    end

    if present ~= 0 and present ~= 5 then
        return redis.error_reply("post projection incomplete")
    end

-- 未初始化则创建新的排序索引
    if present == 0 then
        local initialHot =
            (tonumber(ARGV[4]) - tonumber(ARGV[5])) / tonumber(ARGV[6])

        redis.call("ZADD", KEYS[2], 0, id)
        redis.call("ZADD", KEYS[3], ARGV[3], id)
        redis.call("ZADD", KEYS[4], ARGV[3], id)
        redis.call("ZADD", KEYS[5], initialHot, id)
        redis.call("ZADD", KEYS[6], initialHot, id)
    end
end

-- 没传{userID、direction}不需要同步投票
if #ARGV == 6 then
    return 0
end

local score = tonumber(redis.call("ZSCORE", KEYS[2], id))
if not score then
    return 1
end

-- 根据 Redis 旧状态与 MySQL 当前状态计算净变化
local changed = false

for i = 7, #ARGV, 2 do
    local userID = ARGV[i]
    local new = tonumber(ARGV[i + 1])
    local old = redis.call("HGET", KEYS[1], userID)

    if old == false then
        old = 0
    else
        old = tonumber(old)
    end

    if old ~= new then
        if new == 0 then
            redis.call("HDEL", KEYS[1], userID)
        else
            redis.call("HSET", KEYS[1], userID, new)
        end

        score = score + new - old
        changed = true
    end
end

-- 更新排序
if changed then
    redis.call("ZADD", KEYS[2], score, id)

    local sign = 0
    if score > 0 then
        sign = 1
    elseif score < 0 then
        sign = -1
    end

    local order = math.log10(math.max(math.abs(score), 1))
    local seconds = tonumber(ARGV[4]) - tonumber(ARGV[5])
    local hot = sign * order + seconds / tonumber(ARGV[6])

    redis.call("ZADD", KEYS[5], hot, id)
    redis.call("ZADD", KEYS[6], hot, id)
end

return 0
`

var syncPostScript = redis.NewScript(syncPostScriptSource)

var ErrPostProjectionNotInitialized = errors.New("帖子投影尚未初始化")

// SyncPosts 批量同步帖子，返回失败帖子的错误。
func (s *PostStore) SyncPosts(
	ctx context.Context,
	posts []PostSync,
) map[uint]error {
	failed := make(map[uint]error)

	if len(posts) == 0 {
		return failed
	}

	cmds := make([]*redis.Cmd, len(posts))

	_, _ = s.rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
		// 将 Lua 脚本加载到 Redis 缓存，返回 SHA
		pipe.ScriptLoad(ctx, syncPostScriptSource)

		for i, post := range posts {
			// 构造 Lua 脚本的键值对参数
			keys, args := postSyncArgs(post)

			// 根据 SHA 找到已缓存的 Lua 脚本并执行
			cmds[i] = syncPostScript.EvalSha(ctx, pipe, keys, args...)
		}

		return nil
	})

	for i, cmd := range cmds {
		if err := postSyncResult(cmd); err != nil {
			failed[posts[i].PostID] = err
		}
	}

	return failed
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

// postSyncArgs 构造单个帖子的 Lua 参数。
func postSyncArgs(post PostSync) ([]string, []any) {
	id := strconv.FormatUint(uint64(post.PostID), 10)

	op := 0
	if post.NeedIndexSync {
		op = 1
	}
	if post.Deleted {
		op = -1
	}

	keys := []string{
		postVotesKeyPrefix + id,
		postVoteScoreKey,
		postRankKey(nil, postRankTime),
		postRankKey(&post.CommunityID, postRankTime),
		postRankKey(nil, postRankHot),
		postRankKey(&post.CommunityID, postRankHot),
	}

	args := []any{
		id, op,
		post.CreatedAt.UnixMilli(),
		post.CreatedAt.Unix(),
		hotEpoch, hotGravity,
	}

	for userID, direction := range post.Votes {
		args = append(args, userID, direction)
	}

	return keys, args
}

// postSyncResult 解析单个帖子的 Lua 执行结果。
func postSyncResult(cmd *redis.Cmd) error {
	code, err := cmd.Int64()
	if err != nil {
		return err
	}

	switch code {
	case 0:
		return nil
	case 1:
		return ErrPostProjectionNotInitialized
	default:
		return fmt.Errorf("未知投影同步结果: %d", code)
	}
}
