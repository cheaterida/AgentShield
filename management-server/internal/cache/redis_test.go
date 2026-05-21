package cache

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedisClient(t *testing.T) (*RedisClient, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return newRedisClientForTest(rdb), mr
}

func TestRedisClient_GetSet(t *testing.T) {
	rc, _ := newTestRedisClient(t)
	ctx := context.Background()

	if err := rc.Set(ctx, "k1", []byte("hello"), 10*time.Second); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	got, err := rc.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("Get: got %q, want %q", string(got), "hello")
	}
}

func TestRedisClient_GetMiss(t *testing.T) {
	rc, _ := newTestRedisClient(t)
	ctx := context.Background()

	_, err := rc.Get(ctx, "nonexistent")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("expected ErrCacheMiss, got %v", err)
	}
}

func TestRedisClient_Delete(t *testing.T) {
	rc, _ := newTestRedisClient(t)
	ctx := context.Background()

	rc.Set(ctx, "k1", []byte("x"), time.Minute)
	if err := rc.Delete(ctx, "k1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err := rc.Get(ctx, "k1")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("expected ErrCacheMiss after Delete, got %v", err)
	}
}

func TestRedisClient_TTLExpiration(t *testing.T) {
	rc, mr := newTestRedisClient(t)
	ctx := context.Background()

	if err := rc.Set(ctx, "k1", []byte("ephemeral"), 100*time.Millisecond); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	mr.FastForward(200 * time.Millisecond)

	_, err := rc.Get(ctx, "k1")
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("expected ErrCacheMiss after TTL expiry, got %v", err)
	}
}

func TestRedisClient_Health(t *testing.T) {
	rc, mr := newTestRedisClient(t)

	if err := rc.Health(); err != nil {
		t.Fatalf("Health failed on live miniredis: %v", err)
	}
	mr.Close()
	if err := rc.Health(); err == nil {
		t.Error("Health should fail after miniredis is closed")
	}
}

func TestRedisClient_ConcurrentAccess(t *testing.T) {
	rc, _ := newTestRedisClient(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rc.Set(ctx, "k", []byte("v"), time.Minute)
			rc.Get(ctx, "k")
			rc.Delete(ctx, "k")
		}(i)
	}
	wg.Wait()
}

func TestRedisClient_Metrics(t *testing.T) {
	rc, _ := newTestRedisClient(t)
	ctx := context.Background()

	rc.Set(ctx, "k1", []byte("v1"), time.Minute)
	rc.Get(ctx, "k1") // hit
	rc.Get(ctx, "missing") // miss

	m := rc.Metrics(ctx)
	if m.Hits != 1 {
		t.Errorf("Hits: got %d, want 1", m.Hits)
	}
	if m.Misses != 1 {
		t.Errorf("Misses: got %d, want 1", m.Misses)
	}
	if m.HitRate != 0.5 {
		t.Errorf("HitRate: got %f, want 0.5", m.HitRate)
	}
}

func TestMetricsHandler(t *testing.T) {
	rc, _ := newTestRedisClient(t)
	ctx := context.Background()
	rc.Set(ctx, "k1", []byte("v1"), time.Minute)
	rc.Get(ctx, "k1")

	req := httptest.NewRequest(http.MethodGet, "/debug/metrics", nil)
	rec := httptest.NewRecorder()
	rc.MetricsHandler().ServeHTTP(rec, req)

	resp := rec.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want %d", resp.StatusCode, http.StatusOK)
	}
	ct := resp.Header.Get("Content-Type")
	if ct != "text/plain; version=0.0.4" {
		t.Errorf("Content-Type: got %q", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(body) == 0 {
		t.Error("empty response body")
	}
}
