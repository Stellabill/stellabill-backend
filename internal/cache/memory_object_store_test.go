package cache

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryObjectStore_Put_Get(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx := context.Background()

	key := "test/key"
	data := []byte("test data")

	// Put
	returnedKey, err := store.Put(ctx, key, data)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if returnedKey != key {
		t.Errorf("Put returned key: got %q, want %q", returnedKey, key)
	}

	// Get
	retrieved, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(retrieved, data) {
		t.Errorf("Get returned data: got %q, want %q", retrieved, data)
	}
}

func TestMemoryObjectStore_Get_NotFound(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	if err == nil {
		t.Error("Get should return error for nonexistent key")
	}
	var notFound ErrNotFound
	if !errors.As(err, &notFound) {
		t.Errorf("Get should return ErrNotFound, got %T", err)
	}
}

func TestMemoryObjectStore_Delete(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx := context.Background()

	key := "test/key"
	data := []byte("test data")

	// Put
	store.Put(ctx, key, data)

	// Delete
	err := store.Delete(ctx, key)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deleted
	_, err = store.Get(ctx, key)
	var notFound ErrNotFound
	if !errors.As(err, &notFound) {
		t.Error("Get after Delete should return ErrNotFound")
	}
}

func TestMemoryObjectStore_Delete_NotFound(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx := context.Background()

	// Delete nonexistent (should be idempotent)
	err := store.Delete(ctx, "nonexistent")
	var notFound ErrNotFound
	if !errors.As(err, &notFound) {
		t.Error("Delete should return ErrNotFound for nonexistent key")
	}
}

func TestMemoryObjectStore_ContextCancellation(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Put with cancelled context
	_, err := store.Put(ctx, "key", []byte("data"))
	if err == nil {
		t.Error("Put should fail with cancelled context")
	}

	// Get with cancelled context
	_, err = store.Get(ctx, "key")
	if err == nil {
		t.Error("Get should fail with cancelled context")
	}

	// Delete with cancelled context
	err = store.Delete(ctx, "key")
	if err == nil {
		t.Error("Delete should fail with cancelled context")
	}
}

func TestMemoryObjectStore_ContextTimeout(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx, cancel := context.WithTimeout(context.Background(), -1*time.Nanosecond)
	defer cancel()

	_, err := store.Put(ctx, "key", []byte("data"))
	if err == nil {
		t.Error("Put should fail with expired timeout")
	}
}

func TestMemoryObjectStore_DataIsolation(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx := context.Background()

	key := "test/key"
	originalData := []byte("test data")

	// Put
	store.Put(ctx, key, originalData)

	// Mutate original
	originalData[0] = 'X'

	// Get should return unmodified data
	retrieved, _ := store.Get(ctx, key)
	if retrieved[0] != 't' {
		t.Error("Get returned mutated data; data isolation failed")
	}

	// Mutate retrieved
	retrieved[0] = 'Y'

	// Next Get should return unmodified data
	retrieved2, _ := store.Get(ctx, key)
	if retrieved2[0] != 't' {
		t.Error("Get returned mutated data on second call; data isolation failed")
	}
}

func TestMemoryObjectStore_Concurrent_PutGet(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx := context.Background()

	// Concurrently Put and Get
	done := make(chan error, 20)

	for i := 0; i < 10; i++ {
		go func(id int) {
			key := "key"
			data := []byte{byte(id)}
			_, err := store.Put(ctx, key, data)
			done <- err
		}(i)
	}

	for i := 0; i < 10; i++ {
		go func() {
			_, err := store.Get(ctx, "key")
			done <- err
		}()
	}

	for i := 0; i < 20; i++ {
		err := <-done
		// Either success or data race handled by mutex
		_ = err
	}
}

func TestMemoryObjectStore_Clear(t *testing.T) {
	store := NewMemoryObjectStore()
	ctx := context.Background()

	store.Put(ctx, "key1", []byte("data1"))
	store.Put(ctx, "key2", []byte("data2"))

	all := store.All()
	if len(all) != 2 {
		t.Errorf("All before Clear: got %d, want 2", len(all))
	}

	store.Clear()

	all = store.All()
	if len(all) != 0 {
		t.Errorf("All after Clear: got %d, want 0", len(all))
	}
}
