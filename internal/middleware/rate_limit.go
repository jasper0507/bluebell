package middleware

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/jasper0507/bluebell/internal/response"
)

// sweepInterval 清理令牌桶的最小间隔
const sweepInterval = time.Minute

// ipLimiter 为每个客户端 IP 维护一个令牌桶
type ipLimiter struct {
	mu        sync.Mutex
	limiters  map[string]*rate.Limiter
	limit     rate.Limit
	burst     int
	lastSweep time.Time
}

// RateLimit 中间件，按客户端 IP 进行令牌桶限流
// 每个 IP 每秒补充 r 个令牌，最多积攒 burst 个
func RateLimit(r rate.Limit, burst int) gin.HandlerFunc {
	l := newIPLimiter(r, burst, time.Now())

	return func(c *gin.Context) {
		if !l.allow(c.ClientIP(), time.Now()) {
			response.Error(c, response.CodeTooManyRequests)
			return
		}
	}
}

func newIPLimiter(r rate.Limit, burst int, now time.Time) *ipLimiter {
	return &ipLimiter{
		limiters:  make(map[string]*rate.Limiter),
		limit:     r,
		burst:     burst,
		lastSweep: now,
	}
}

// allow 判断该 IP 在 now 时刻能否取得一个令牌
func (l *ipLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 1. 定期清理已回满的令牌桶，避免 IP 变化导致内存无限增长
	// 回满的令牌桶与新建的等价，丢弃后不影响限流结果
	// 重建 map 而不是原地 delete，因为 Go map 删除元素后不会归还内存
	if now.Sub(l.lastSweep) >= sweepInterval {
		active := make(map[string]*rate.Limiter)
		for ip, limiter := range l.limiters {
			if limiter.TokensAt(now) < float64(l.burst) {
				active[ip] = limiter
			}
		}
		l.limiters = active
		l.lastSweep = now
	}

	// 2. 获取或创建该 IP 的令牌桶
	limiter, ok := l.limiters[ip]
	if !ok {
		limiter = rate.NewLimiter(l.limit, l.burst)
		l.limiters[ip] = limiter
	}

	// 3. 消耗一个令牌
	return limiter.AllowN(now, 1)
}
