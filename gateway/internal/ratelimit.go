package internal

import (
	"sync"
	"time"
)

// RateLimiter 基于令牌桶的每租户限流。
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*tokenBucket
	rate     float64 // tokens per second
	capacity float64
}

type tokenBucket struct {
	tokens   float64
	lastFill time.Time
}

func NewRateLimiter(ratePerSec, burst int) *RateLimiter {
	return &RateLimiter{
		buckets:  make(map[string]*tokenBucket),
		rate:     float64(ratePerSec),
		capacity: float64(burst),
	}
}

// Allow 检查指定 key 是否允许通过。
func (rl *RateLimiter) Allow(key string) bool {
	if key == "" {
		key = "_global"
	}
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[key]
	now := time.Now()
	if !ok {
		b = &tokenBucket{tokens: rl.capacity, lastFill: now}
		rl.buckets[key] = b
	}
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.capacity {
		b.tokens = rl.capacity
	}
	b.lastFill = now

	if b.tokens < 1.0 {
		return false
	}
	b.tokens--
	return true
}

// Cleanup 定期清理长时间不活动的桶（可后台调用）。
func (rl *RateLimiter) Cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	for k, b := range rl.buckets {
		if now.Sub(b.lastFill) > maxAge {
			delete(rl.buckets, k)
		}
	}
}
