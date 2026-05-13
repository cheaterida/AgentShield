package retry

import (
	"context"
	"math"
	"math/rand"
	"time"
)

// Backoff 提供指数退避重试。
type Backoff struct {
	Initial time.Duration // 首次等待，默认 1s
	Max     time.Duration // 最大等待，默认 60s
	Factor  float64       // 倍数，默认 2.0
}

func (b Backoff) withDefaults() Backoff {
	if b.Initial <= 0 {
		b.Initial = time.Second
	}
	if b.Max <= 0 {
		b.Max = 60 * time.Second
	}
	if b.Factor <= 1.0 {
		b.Factor = 2.0
	}
	return b
}

// Do 在 fn 返回 nil 之前重复调用 fn，每次失败后等待退避时间。
// ctx 取消时立即返回 ctx.Err()。
func Do(ctx context.Context, fn func() error, b Backoff) error {
	b = b.withDefaults()
	attempt := 0
	for {
		err := fn()
		if err == nil {
			return nil
		}
		attempt++
		wait := time.Duration(float64(b.Initial) * math.Pow(b.Factor, float64(attempt-1)))
		if wait > b.Max {
			wait = b.Max
		}
		// ±25% jitter
		jitter := time.Duration(rand.Int63n(int64(wait) / 4))
		wait = wait - wait/8 + jitter
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}
