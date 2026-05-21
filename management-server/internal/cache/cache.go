package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrCacheMiss      = errors.New("cache: key not found")
	ErrCacheUnhealthy = errors.New("cache: health check failed")
)

// Cache is a generic byte-level cache interface.
// Consumers (Track B) serialize their own values before calling Set,
// and deserialize after calling Get.
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	Delete(ctx context.Context, key string) error
	Health() error
}

// Marshal encodes v to JSON bytes.
func Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Unmarshal decodes JSON bytes into v.
func Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
