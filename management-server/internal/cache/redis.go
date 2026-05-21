package cache

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisClient struct {
	client  *redis.Client
	hits    atomic.Int64
	misses  atomic.Int64
}

// NewRedisClient creates a Redis-backed Cache. It calls PING on creation
// and returns an error if the server is unreachable.
func NewRedisClient(addr, password string, db int) (*RedisClient, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolTimeout:  4 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	return &RedisClient{client: rdb}, nil
}

// newRedisClientForTest wraps an already-connected client without pinging.
// Only exported for use by internal tests and integration tests.
func newRedisClientForTest(rdb *redis.Client) *RedisClient {
	return &RedisClient{client: rdb}
}

func (r *RedisClient) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		r.misses.Add(1)
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	r.hits.Add(1)
	return b, nil
}

func (r *RedisClient) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisClient) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *RedisClient) Health() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := r.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("%w: %v", ErrCacheUnhealthy, err)
	}
	return nil
}

func (r *RedisClient) Close() error {
	return r.client.Close()
}

// ── metrics ──

type CacheMetrics struct {
	Hits       int64   `json:"hits"`
	Misses     int64   `json:"misses"`
	HitRate    float64 `json:"hit_rate"`
	TotalKeys  int64   `json:"total_keys"`
	MemoryUsed int64   `json:"memory_used_bytes"`
	PoolSize   int     `json:"pool_size"`
	IdleConns  int     `json:"idle_conns"`
	TotalConns int     `json:"total_conns"`
}

func (r *RedisClient) Metrics(ctx context.Context) CacheMetrics {
	m := CacheMetrics{}
	m.Hits = r.hits.Load()
	m.Misses = r.misses.Load()
	total := m.Hits + m.Misses
	if total > 0 {
		m.HitRate = float64(m.Hits) / float64(total)
	}
	if info, err := r.client.Info(ctx, "keyspace").Result(); err == nil {
		var keys, expires int64
		fmt.Sscanf(info, `db0:keys=%d,expires=%d`, &keys, &expires)
		m.TotalKeys = keys
	}
	if info, err := r.client.Info(ctx, "memory").Result(); err == nil {
		var mem int64
		fmt.Sscanf(info, "used_memory:%d", &mem)
		m.MemoryUsed = mem
	}
	ps := r.client.PoolStats()
	m.PoolSize = int(ps.TotalConns)
	m.IdleConns = int(ps.IdleConns)
	m.TotalConns = int(ps.TotalConns)
	return m
}

func (r *RedisClient) MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		m := r.Metrics(req.Context())
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		fmt.Fprintf(w, `# HELP agentshield_cache_hits_total Total cache hits.
# TYPE agentshield_cache_hits_total counter
agentshield_cache_hits_total %d
# HELP agentshield_cache_misses_total Total cache misses.
# TYPE agentshield_cache_misses_total counter
agentshield_cache_misses_total %d
# HELP agentshield_cache_hit_rate Cache hit rate.
# TYPE agentshield_cache_hit_rate gauge
agentshield_cache_hit_rate %.3f
# HELP agentshield_cache_keys_total Registered keys in Redis.
# TYPE agentshield_cache_keys_total gauge
agentshield_cache_keys_total %d
# HELP agentshield_cache_memory_bytes Redis memory usage.
# TYPE agentshield_cache_memory_bytes gauge
agentshield_cache_memory_bytes %d
# HELP agentshield_cache_pool_size Redis connection pool size.
# TYPE agentshield_cache_pool_size gauge
agentshield_cache_pool_size %d
`, m.Hits, m.Misses, m.HitRate, m.TotalKeys, m.MemoryUsed, m.PoolSize)
	})
}
