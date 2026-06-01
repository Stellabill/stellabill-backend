package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"stellarbill-backend/internal/cache"
)

// --- helpers ---

type faultyCacheForSub struct{}

func (f *faultyCacheForSub) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("cache down")
}
func (f *faultyCacheForSub) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return errors.New("cache down")
}
func (f *faultyCacheForSub) Delete(_ context.Context, _ string) error {
	return errors.New("cache down")
}

// --- tests ---

func TestCachedSubscriptionRepo_HitMiss(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-1", PlanID: "plan-1", Status: "active",
		Amount: "1000", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// First read → miss
	sr, err := csr.FindByID(ctx, "sub-1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if sr.ID != "sub-1" {
		t.Fatalf("expected sub-1, got %s", sr.ID)
	}
	_, misses := csr.Metrics()
	if misses == 0 {
		t.Fatal("expected at least one miss")
	}

	// Second read → hit
	_, err = csr.FindByID(ctx, "sub-1")
	if err != nil {
		t.Fatalf("second FindByID: %v", err)
	}
	hits, _ := csr.Metrics()
	if hits == 0 {
		t.Fatal("expected cache hit on second read")
	}
}

func TestCachedSubscriptionRepo_FindByIDAndTenant(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-2", TenantID: "tenant-A", Status: "active",
		Amount: "500", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// Miss then hit
	sr, err := csr.FindByIDAndTenant(ctx, "sub-2", "tenant-A")
	if err != nil || sr.ID != "sub-2" {
		t.Fatalf("unexpected: %v %v", sr, err)
	}
	sr2, err := csr.FindByIDAndTenant(ctx, "sub-2", "tenant-A")
	if err != nil || sr2.ID != "sub-2" {
		t.Fatalf("cached read: %v %v", sr2, err)
	}
	hits, _ := csr.Metrics()
	if hits == 0 {
		t.Fatal("expected cache hit on repeated tenant lookup")
	}
}

func TestCachedSubscriptionRepo_CacheOutageFallback(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-3", Status: "active", Amount: "999", Currency: "usd", Interval: "year",
	})
	csr := NewCachedSubscriptionRepo(backend, &faultyCacheForSub{}, time.Minute)

	sr, err := csr.FindByID(ctx, "sub-3")
	if err != nil || sr.ID != "sub-3" {
		t.Fatalf("expected fallback to backend, got %v %v", sr, err)
	}
}

func TestCachedSubscriptionRepo_Flush_Empty(t *testing.T) {
	ctx := context.Background()
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(NewMockSubscriptionRepo(), mem, time.Minute)

	n, err := csr.Flush(ctx)
	if err != nil || n != 0 {
		t.Fatalf("flush empty: want 0,nil got %d,%v", n, err)
	}
}

func TestCachedSubscriptionRepo_Flush_WithEntries(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(
		&SubscriptionRow{ID: "sub-4", Status: "active", Amount: "100", Currency: "usd", Interval: "month"},
		&SubscriptionRow{ID: "sub-5", Status: "active", Amount: "200", Currency: "usd", Interval: "year"},
	)
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// Populate cache
	_, _ = csr.FindByID(ctx, "sub-4")
	_, _ = csr.FindByID(ctx, "sub-5")
	if mem.Len() != 2 {
		t.Fatalf("expected 2 cached entries before flush, got %d", mem.Len())
	}

	n, err := csr.Flush(ctx)
	if err != nil {
		t.Fatalf("Flush error: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 keys flushed, got %d", n)
	}
	if mem.Len() != 0 {
		t.Fatalf("expected empty cache after flush, got %d", mem.Len())
	}
}

func TestCachedSubscriptionRepo_Flush_Idempotent(t *testing.T) {
	ctx := context.Background()
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(NewMockSubscriptionRepo(), mem, time.Minute)

	n1, err1 := csr.Flush(ctx)
	n2, err2 := csr.Flush(ctx)
	if err1 != nil || err2 != nil {
		t.Fatalf("Flush errors: %v %v", err1, err2)
	}
	if n1 != 0 || n2 != 0 {
		t.Fatalf("expected 0,0 on repeated empty flush, got %d,%d", n1, n2)
	}
}

func TestCachedSubscriptionRepo_ResetMetrics(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-6", Status: "active", Amount: "1", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	_, _ = csr.FindByID(ctx, "sub-6") // miss
	_, _ = csr.FindByID(ctx, "sub-6") // hit

	hits, misses := csr.Metrics()
	if hits == 0 || misses == 0 {
		t.Fatalf("expected non-zero hits/misses before reset, got %d/%d", hits, misses)
	}

	csr.ResetMetrics()

	hits2, misses2 := csr.Metrics()
	if hits2 != 0 || misses2 != 0 {
		t.Fatalf("expected 0/0 after ResetMetrics, got %d/%d", hits2, misses2)
	}
}

func TestCachedSubscriptionRepo_Namespace(t *testing.T) {
	csr := NewCachedSubscriptionRepo(NewMockSubscriptionRepo(), cache.NewInMemory(), time.Minute)
	if csr.Namespace() != "subscriptions" {
		t.Fatalf("expected 'subscriptions', got %q", csr.Namespace())
	}
}

func TestCachedSubscriptionRepo_Concurrent(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-7", Status: "active", Amount: "77", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_, err := csr.FindByID(ctx, "sub-7")
				if err != nil {
					t.Errorf("concurrent FindByID: %v", err)
					return
				}
			}
		}()
	}
	// Flush concurrently while readers are running
	wg.Add(1)
	go func() {
		defer wg.Done()
		for k := 0; k < 5; k++ {
			_, _ = csr.Flush(ctx)
			time.Sleep(2 * time.Millisecond)
		}
	}()
	wg.Wait()
}

func TestCachedSubscriptionRepo_ImplementsPurgeable(t *testing.T) {
	var _ cache.Purgeable = NewCachedSubscriptionRepo(NewMockSubscriptionRepo(), cache.NewInMemory(), time.Minute)
}
