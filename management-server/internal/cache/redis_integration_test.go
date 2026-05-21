//go:build integration
// +build integration

package cache

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestIntegration_RedisRealConnection(t *testing.T) {
	addr := os.Getenv("AGENTSHIELD_REDIS_ADDR")
	if addr == "" {
		t.Skip("AGENTSHIELD_REDIS_ADDR not set")
	}
	password := os.Getenv("AGENTSHIELD_REDIS_PASSWORD")

	rc, err := NewRedisClient(addr, password, 0)
	if err != nil {
		t.Fatalf("NewRedisClient failed: %v", err)
	}
	defer rc.Close()

	if err := rc.Health(); err != nil {
		t.Fatalf("Health failed: %v", err)
	}
}

func TestIntegration_RedisLargeValue(t *testing.T) {
	addr := os.Getenv("AGENTSHIELD_REDIS_ADDR")
	if addr == "" {
		t.Skip("AGENTSHIELD_REDIS_ADDR not set")
	}

	rc, err := NewRedisClient(addr, "", 0)
	if err != nil {
		t.Fatalf("NewRedisClient failed: %v", err)
	}
	defer rc.Close()

	ctx := context.Background()
	val := make([]byte, 1024*1024) // 1MB
	for i := range val {
		val[i] = byte(i % 256)
	}

	if err := rc.Set(ctx, "integration:large", val, 10*time.Second); err != nil {
		t.Fatalf("Set large value failed: %v", err)
	}
	got, err := rc.Get(ctx, "integration:large")
	if err != nil {
		t.Fatalf("Get large value failed: %v", err)
	}
	if len(got) != len(val) {
		t.Errorf("length mismatch: got %d, want %d", len(got), len(val))
	}
	rc.Delete(ctx, "integration:large")
}

func TestIntegration_RedisMetricsReal(t *testing.T) {
	addr := os.Getenv("AGENTSHIELD_REDIS_ADDR")
	if addr == "" {
		t.Skip("AGENTSHIELD_REDIS_ADDR not set")
	}

	rc, err := NewRedisClient(addr, "", 0)
	if err != nil {
		t.Fatalf("NewRedisClient failed: %v", err)
	}
	defer rc.Close()

	ctx := context.Background()
	rc.Set(ctx, "integration:metrics:test", []byte("x"), time.Minute)
	rc.Get(ctx, "integration:metrics:test")
	rc.Get(ctx, "integration:metrics:missing")

	m := rc.Metrics(ctx)
	if m.Hits < 1 {
		t.Errorf("expected at least 1 hit, got %d", m.Hits)
	}
	if m.Misses < 1 {
		t.Errorf("expected at least 1 miss, got %d", m.Misses)
	}
	rc.Delete(ctx, "integration:metrics:test")
}
