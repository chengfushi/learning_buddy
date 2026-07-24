// package service —— 用户级 AI 调用限流；Redis 可用时使用原子滑动窗口，故障时降级到进程内窗口。
package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"learning_buddy/backend/internal/config"
)

const slidingWindowScript = `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)
local count = redis.call('ZCARD', key)
if count >= limit then return 0 end
redis.call('ZADD', key, now, ARGV[4])
redis.call('EXPIRE', key, math.ceil(window / 1000) + 1)
return 1
`

type RateLimiter struct {
	redis  *redis.Client
	limit  int
	limits map[string]int
	window time.Duration
	mu     sync.Mutex
	local  map[string][]time.Time
}

func NewRateLimiter(cfg *config.Config) *RateLimiter {
	r := &RateLimiter{
		limit: cfg.ChatRateLimitPerMin,
		limits: map[string]int{
			"chat": cfg.ChatRateLimitPerMin,
			"plan": cfg.PlanRateLimitPerMin,
			"quiz": cfg.QuizRateLimitPerMin,
		},
		window: time.Minute,
		local:  make(map[string][]time.Time),
	}
	if r.limit <= 0 {
		r.limit = 20
	}
	for name, limit := range r.limits {
		if limit <= 0 {
			r.limits[name] = r.limit
		}
	}
	if cfg.RedisAddr != "" {
		r.redis = redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, DialTimeout: 200 * time.Millisecond, ReadTimeout: 200 * time.Millisecond, WriteTimeout: 200 * time.Millisecond})
	}
	return r
}

func (r *RateLimiter) AllowChat(ctx context.Context, userID int64) (bool, error) {
	return r.Allow(ctx, userID, "chat")
}

// Allow applies the user-scoped sliding window to every AI feature.
func (r *RateLimiter) Allow(ctx context.Context, userID int64, service string) (bool, error) {
	key := fmt.Sprintf("ratelimit:%s:%d", service, userID)
	limit := r.limits[service]
	if limit <= 0 {
		limit = r.limit
	}
	now := time.Now()
	if r.redis != nil {
		result, err := r.redis.Eval(ctx, slidingWindowScript, []string{key}, now.UnixMilli(), r.window.Milliseconds(), limit, uuid.NewString()).Int()
		if err == nil {
			return result == 1, nil
		}
	}
	return r.allowLocal(key, now, limit), nil
}

func (r *RateLimiter) allowLocal(key string, now time.Time, limit int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := now.Add(-r.window)
	items := r.local[key][:0]
	for _, item := range r.local[key] {
		if item.After(cutoff) {
			items = append(items, item)
		}
	}
	if len(items) >= limit {
		r.local[key] = items
		return false
	}
	r.local[key] = append(items, now)
	return true
}
