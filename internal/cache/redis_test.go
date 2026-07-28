package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// setupTestRedis creates a miniredis server and returns a Redis cache backed by it.
func setupTestRedis(t *testing.T) (*Redis, *miniredis.Miniredis) {
	t.Helper()
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { client.Close() })
	return NewRedis(client), s
}

func TestRedis_GetMiss(t *testing.T) {
	ctx := context.Background()
	r, _ := setupTestRedis(t)

	val, err := r.Get(ctx, "missing")
	if err != nil {
		t.Fatalf("unexpected error on miss: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil on miss, got %v", val)
	}
}

func TestRedis_SetAndGet(t *testing.T) {
	ctx := context.Background()
	r, s := setupTestRedis(t)

	// Set a value
	err := r.Set(ctx, "plan:byid:abc", []byte(`{"id":"abc"}`), time.Minute)
	if err != nil {
		t.Fatalf("unexpected Set error: %v", err)
	}

	// Verify it's in Redis
	s.CheckGet(t, "plan:byid:abc", `{"id":"abc"}`)

	// Get should return it
	val, err := r.Get(ctx, "plan:byid:abc")
	if err != nil {
		t.Fatalf("unexpected Get error: %v", err)
	}
	if string(val) != `{"id":"abc"}` {
		t.Fatalf("expected %q, got %q", `{"id":"abc"}`, string(val))
	}
}

func TestRedis_TTL(t *testing.T) {
	ctx := context.Background()
	r, s := setupTestRedis(t)

	// Set with short TTL
	err := r.Set(ctx, "ephemeral", []byte("data"), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("unexpected Set error: %v", err)
	}

	// Should be present
	val, err := r.Get(ctx, "ephemeral")
	if err != nil || val == nil {
		t.Fatalf("expected value before TTL expiry")
	}

	// Fast-forward past TTL
	s.FastForward(60 * time.Millisecond)

	// Should be expired now
	val, err = r.Get(ctx, "ephemeral")
	if err != nil {
		t.Fatalf("unexpected error after expiry: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil after TTL expiry, got %v", val)
	}
}

func TestRedis_Delete(t *testing.T) {
	ctx := context.Background()
	r, _ := setupTestRedis(t)

	_ = r.Set(ctx, "delete-me", []byte("data"), time.Minute)

	// Verify present
	val, _ := r.Get(ctx, "delete-me")
	if val == nil {
		t.Fatalf("expected value before delete")
	}

	// Delete
	err := r.Delete(ctx, "delete-me")
	if err != nil {
		t.Fatalf("unexpected Delete error: %v", err)
	}

	// Should be gone
	val, err = r.Get(ctx, "delete-me")
	if err != nil {
		t.Fatalf("unexpected error after delete: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil after delete, got %v", val)
	}
}

func TestRedis_Overwrite(t *testing.T) {
	ctx := context.Background()
	r, _ := setupTestRedis(t)

	_ = r.Set(ctx, "key", []byte("old"), time.Minute)
	_ = r.Set(ctx, "key", []byte("new"), time.Minute)

	val, _ := r.Get(ctx, "key")
	if string(val) != "new" {
		t.Fatalf("expected 'new', got %q", string(val))
	}
}

func TestRedis_ZeroTTL(t *testing.T) {
	ctx := context.Background()
	r, s := setupTestRedis(t)

	// Zero TTL means no expiration in Redis
	err := r.Set(ctx, "persistent", []byte("stays"), 0)
	if err != nil {
		t.Fatalf("unexpected Set error: %v", err)
	}

	val, _ := r.Get(ctx, "persistent")
	if string(val) != "stays" {
		t.Fatalf("expected 'stays', got %q", string(val))
	}

	// Even after fast-forward, it should still be there
	s.FastForward(time.Hour)
	val, _ = r.Get(ctx, "persistent")
	if string(val) != "stays" {
		t.Fatalf("expected 'stays' after fast-forward, got %q", string(val))
	}
}

func TestRedis_Ping(t *testing.T) {
	r, _ := setupTestRedis(t)
	err := r.Ping(context.Background())
	if err != nil {
		t.Fatalf("expected Ping to succeed, got: %v", err)
	}
}

func TestRedis_ConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	r, _ := setupTestRedis(t)

	const workers = 20
	const iterations = 50
	done := make(chan bool, workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			key := "concurrent-key"
			for j := 0; j < iterations; j++ {
				// Mix of reads and writes
				if j%2 == 0 {
					_ = r.Set(ctx, key, []byte("value"), time.Minute)
				} else {
					_, _ = r.Get(ctx, key)
				}
			}
			done <- true
		}(i)
	}

	for i := 0; i < workers; i++ {
		<-done
	}
}

func TestRedis_NewRedisFromURL(t *testing.T) {
	// We can test parsing without actually connecting
	// A real connection test would fail, but parsing should work
	s := miniredis.RunT(t)
	url := "redis://" + s.Addr() + "/0"

	r, err := NewRedisFromURL(url)
	if err != nil {
		t.Fatalf("unexpected error parsing URL: %v", err)
	}

	// Verify it works
	ctx := context.Background()
	err = r.Set(ctx, "url-key", []byte("url-value"), time.Minute)
	if err != nil {
		t.Fatalf("unexpected Set error with URL-based client: %v", err)
	}

	val, _ := r.Get(ctx, "url-key")
	if string(val) != "url-value" {
		t.Fatalf("expected 'url-value', got %q", string(val))
	}
}

func TestRedis_TransientErrorDegradation(t *testing.T) {
	// Create a Redis client that points to a closed/refused connection
	// This simulates Redis being unavailable, and the implementation
	// should gracefully degrade rather than propagate the error.
	client := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1", // unlikely to be listening
		DialTimeout: 100 * time.Millisecond,
	})
	t.Cleanup(func() { client.Close() })
	r := NewRedis(client)
	ctx := context.Background()

	// Get should return (nil, nil) on connection error
	val, err := r.Get(ctx, "any-key")
	if err != nil {
		t.Fatalf("expected nil error on transient failure, got: %v", err)
	}
	if val != nil {
		t.Fatalf("expected nil value on transient failure, got %v", val)
	}

	// Set should not return error
	err = r.Set(ctx, "any-key", []byte("data"), time.Minute)
	if err != nil {
		t.Fatalf("expected nil error on Set transient failure, got: %v", err)
	}

	// Delete should not return error
	err = r.Delete(ctx, "any-key")
	if err != nil {
		t.Fatalf("expected nil error on Delete transient failure, got: %v", err)
	}
}

func TestRedis_EmptyValue(t *testing.T) {
	ctx := context.Background()
	r, _ := setupTestRedis(t)

	// Setting an empty value should still work
	err := r.Set(ctx, "empty", []byte{}, time.Minute)
	if err != nil {
		t.Fatalf("unexpected Set error for empty value: %v", err)
	}

	val, err := r.Get(ctx, "empty")
	if err != nil {
		t.Fatalf("unexpected Get error for empty value: %v", err)
	}
	if val == nil || len(val) != 0 {
		t.Fatalf("expected empty byte slice, got %v", val)
	}
}

func TestRedis_BinaryData(t *testing.T) {
	ctx := context.Background()
	r, _ := setupTestRedis(t)

	binary := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE}
	err := r.Set(ctx, "binary", binary, time.Minute)
	if err != nil {
		t.Fatalf("unexpected Set error for binary data: %v", err)
	}

	val, err := r.Get(ctx, "binary")
	if err != nil {
		t.Fatalf("unexpected Get error for binary data: %v", err)
	}
	if len(val) != len(binary) {
		t.Fatalf("expected %d bytes, got %d", len(binary), len(val))
	}
	for i := range binary {
		if val[i] != binary[i] {
			t.Fatalf("byte %d: expected %d, got %d", i, binary[i], val[i])
		}
	}
}
