package cache

import (
	"context"
	"stellarbill-backend/internal/servertiming"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis implements the Cache interface backed by a Redis server.
// On any error it returns (nil, nil) from Get so the caller falls through to
// the database rather than propagating the error. Set and Delete also swallow
// errors for the same reason — the caller always has the DB as a fallback.
type Redis struct {
	client redis.UniversalClient
}

// Compile-time check that Redis satisfies Cache.
var _ Cache = (*Redis)(nil)

// NewRedis returns a Redis cache backed by the supplied client.
func NewRedis(client redis.UniversalClient) *Redis {
	return &Redis{client: client}
}

// NewRedisFromURL parses a Redis URL and returns a Redis cache.
// Example URL: "redis://localhost:6379/0"
func NewRedisFromURL(url string) (*Redis, error) {
	opts, err := redis.ParseURL(url)
	if err != nil {
		return nil, err
	}
	return NewRedis(redis.NewClient(opts)), nil
}

func (r *Redis) Get(ctx context.Context, key string) ([]byte, error) {
	start := time.Now()
	defer func() {
		if rec := servertiming.FromContext(ctx); rec != nil {
			rec.RecordCache(time.Since(start))
		}
	}()
	val, err := r.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return nil, nil // cache miss
	}
	if err != nil {
		// Return nil so caller falls through to DB — safe degradation.
		return nil, nil
	}
	return val, nil
}

func (r *Redis) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	start := time.Now()
	defer func() {
		if rec := servertiming.FromContext(ctx); rec != nil {
			rec.RecordCache(time.Since(start))
		}
	}()
	err := r.client.Set(ctx, key, value, ttl).Err()
	if err != nil {
		// Non-fatal: stale data is acceptable, DB fallback works.
		return nil
	}
	return nil
}

func (r *Redis) Delete(ctx context.Context, key string) error {
	start := time.Now()
	defer func() {
		if rec := servertiming.FromContext(ctx); rec != nil {
			rec.RecordCache(time.Since(start))
		}
	}()
	err := r.client.Del(ctx, key).Err()
	if err != nil {
		// Non-fatal: stale data will be detected by the stale-read path.
		return nil
	}
	return nil
}

// Ping checks Redis connectivity. Used for health checks.
func (r *Redis) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

// Close shuts down the underlying Redis client.
func (r *Redis) Close() error {
	return r.client.Close()
}
