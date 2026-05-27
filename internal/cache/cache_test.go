package cache

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestInMemory_BasicSetGetDelete(t *testing.T) {
	c := NewInMemory()
	ctx := context.Background()

	// Miss
	if v, err := c.Get(ctx, "missing"); err != nil || v != nil {
		t.Fatalf("expected nil miss, got %v err=%v", v, err)
	}

	// Set + Get (no TTL)
	if err := c.Set(ctx, "k", []byte("v"), 0); err != nil {
		t.Fatal(err)
	}
	v, err := c.Get(ctx, "k")
	if err != nil || string(v) != "v" {
		t.Fatalf("get got %q err=%v", v, err)
	}

	// Set with TTL, then expire
	if err := c.Set(ctx, "exp", []byte("x"), 1*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if v, _ := c.Get(ctx, "exp"); v != nil {
		t.Fatalf("expected expired entry to be nil, got %q", v)
	}

	// Delete
	if err := c.Delete(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if v, _ := c.Get(ctx, "k"); v != nil {
		t.Fatalf("expected delete to remove, got %q", v)
	}
}

func TestGuardedCache_GetOrLoad(t *testing.T) {
	g := NewGuardedCache(NewInMemory())
	ctx := context.Background()

	// First call: loader runs
	calls := 0
	loader := func() ([]byte, error) {
		calls++
		return []byte("data"), nil
	}

	v, err := g.GetOrLoad(ctx, "k1", time.Minute, loader)
	if err != nil || string(v) != "data" {
		t.Fatalf("got %q err=%v", v, err)
	}

	// Second call: cache hit, no loader
	v, err = g.GetOrLoad(ctx, "k1", time.Minute, loader)
	if err != nil || string(v) != "data" {
		t.Fatalf("got %q err=%v", v, err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 loader call, got %d", calls)
	}

	// Loader error path
	_, err = g.GetOrLoad(ctx, "err-key", time.Minute, func() ([]byte, error) {
		return nil, errors.New("boom")
	})
	if err == nil {
		t.Fatal("expected error from loader")
	}

	// Delete
	if err := g.Delete(ctx, "k1"); err != nil {
		t.Fatal(err)
	}
}
