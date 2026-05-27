package cache

import (
	"context"
	"testing"
	"time"
)

func TestInMemory_GetSetDelete(t *testing.T) {
	ctx := context.Background()
	m := NewInMemory()

	// Get on empty cache returns nil, nil
	v, err := m.Get(ctx, "k1")
	if err != nil || v != nil {
		t.Fatalf("expected nil,nil on empty cache, got %v,%v", v, err)
	}

	// Set then Get
	if err := m.Set(ctx, "k1", []byte("hello"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err = m.Get(ctx, "k1")
	if err != nil || string(v) != "hello" {
		t.Fatalf("expected hello, got %v %v", string(v), err)
	}

	// Delete then Get
	if err := m.Delete(ctx, "k1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	v, err = m.Get(ctx, "k1")
	if err != nil || v != nil {
		t.Fatalf("expected nil after delete, got %v %v", v, err)
	}
}

func TestInMemory_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	m := NewInMemory()

	_ = m.Set(ctx, "ttl", []byte("data"), 30*time.Millisecond)

	// Should be present immediately
	v, _ := m.Get(ctx, "ttl")
	if string(v) != "data" {
		t.Fatalf("expected data before expiry")
	}

	time.Sleep(50 * time.Millisecond)

	// Should be evicted after TTL
	v, _ = m.Get(ctx, "ttl")
	if v != nil {
		t.Fatalf("expected nil after TTL expiry, got %s", v)
	}
}

func TestInMemory_Flush(t *testing.T) {
	ctx := context.Background()
	m := NewInMemory()

	_ = m.Set(ctx, "a", []byte("1"), 0)
	_ = m.Set(ctx, "b", []byte("2"), 0)
	_ = m.Set(ctx, "c", []byte("3"), 0)

	if m.Len() != 3 {
		t.Fatalf("expected 3 entries, got %d", m.Len())
	}

	n, err := m.Flush(ctx)
	if err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	if n != 3 {
		t.Fatalf("expected 3 keys flushed, got %d", n)
	}
	if m.Len() != 0 {
		t.Fatalf("expected 0 after Flush, got %d", m.Len())
	}

	// Idempotent: second flush on empty cache returns 0, no error
	n2, err2 := m.Flush(ctx)
	if err2 != nil || n2 != 0 {
		t.Fatalf("second Flush: want 0,nil got %d,%v", n2, err2)
	}
}

func TestInMemory_ImplementsFlushable(t *testing.T) {
	var _ Flushable = NewInMemory()
}
