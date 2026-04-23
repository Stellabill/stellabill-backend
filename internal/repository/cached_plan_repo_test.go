package repository

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"stellarbill-backend/internal/cache"
)

func TestCachedPlanRepo_HitMissAndTTL(t *testing.T) {
	ctx := context.Background()
	backend := NewMockPlanRepo(&PlanRow{ID: "plan-1", Name: "Original", Amount: "1000", Currency: "usd", Interval: "month"})
	mem := cache.NewInMemory()
	cpr := NewCachedPlanRepo(backend, mem, 50*time.Millisecond)

	// First read -> miss
	p, err := cpr.FindByID(ctx, "plan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "Original" {
		t.Fatalf("expected Original, got %s", p.Name)
	}

	hits, misses, stales := cpr.Metrics()
	if misses == 0 {
		t.Fatalf("expected at least one miss, got hits=%d misses=%d stales=%d", hits, misses, stales)
	}

	// Second read -> should hit cache
	p2, err := cpr.FindByID(ctx, "plan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p2.Name != "Original" {
		t.Fatalf("expected Original on cached read, got %s", p2.Name)
	}

	h2, m2, s2 := cpr.Metrics()
	if h2 == 0 {
		t.Fatalf("expected hit > 0 after repeated read, got hits=%d misses=%d stales=%d", h2, m2, s2)
	}

	// Wait for TTL to expire
	time.Sleep(60 * time.Millisecond)

	// Update backend
	backend.records["plan-1"].Name = "Updated"

	// Next read should miss and return updated
	p3, err := cpr.FindByID(ctx, "plan-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p3.Name != "Updated" {
		t.Fatalf("expected Updated after TTL expiry, got %s", p3.Name)
	}
}

// faultyCache simulates cache outages by returning errors on Get/Set/Delete.
type faultyCache struct{}

func (f *faultyCache) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, errors.New("cache down")
}
func (f *faultyCache) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error {
	return errors.New("cache down")
}
func (f *faultyCache) Delete(_ context.Context, _ string) error { return errors.New("cache down") }

func TestCachedPlanRepo_CacheOutageFallback(t *testing.T) {
	ctx := context.Background()
	backend := NewMockPlanRepo(&PlanRow{ID: "plan-2", Name: "B", Amount: "2000", Currency: "usd", Interval: "month"})
	fc := &faultyCache{}
	cpr := NewCachedPlanRepo(backend, fc, time.Minute)

	p, err := cpr.FindByID(ctx, "plan-2")
	if err != nil {
		t.Fatalf("expected fallback to backend, got error: %v", err)
	}
	if p.Name != "B" {
		t.Fatalf("expected B, got %s", p.Name)
	}
}

func TestCachedPlanRepo_ConcurrentInvalidation(t *testing.T) {
	ctx := context.Background()
	backend := NewMockPlanRepo(&PlanRow{ID: "plan-3", Name: "C1", Amount: "3000", Currency: "usd", Interval: "month"})
	mem := cache.NewInMemory()
	cpr := NewCachedPlanRepo(backend, mem, time.Minute)

	// Prime cache
	if _, err := cpr.FindByID(ctx, "plan-3"); err != nil {
		t.Fatalf("prime error: %v", err)
	}

	var wg sync.WaitGroup
	// Start many readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				p, err := cpr.FindByID(ctx, "plan-3")
				if err != nil {
					t.Errorf("reader error: %v", err)
					return
				}
				if p == nil {
					t.Errorf("nil plan")
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	// Invalidate while readers are running and change backend
	time.Sleep(5 * time.Millisecond)
	backend.records["plan-3"].Name = "C2"
	if err := cpr.Delete(ctx, "plan-3"); err != nil {
		t.Fatalf("delete error: %v", err)
	}

	wg.Wait()

	// After invalidation, next read should observe updated value (may be cached again)
	p, err := cpr.FindByID(ctx, "plan-3")
	if err != nil {
		t.Fatalf("final read error: %v", err)
	}
	if p.Name != "C2" {
		t.Fatalf("expected C2 after invalidation, got %s", p.Name)
	}
}

func TestCachedPlanRepo_StaleRead(t *testing.T) {
	ctx := context.Background()
	backend := NewMockPlanRepo(&PlanRow{ID: "plan-1", Name: "Original", Amount: "1000", Currency: "usd", Interval: "month"})
	mem := cache.NewInMemory()
	cpr := NewCachedPlanRepo(backend, mem, time.Minute)

	// Prime cache
	if _, err := cpr.FindByID(ctx, "plan-1"); err != nil {
		t.Fatalf("prime error: %v", err)
	}
	// Second read -> hit
	if _, err := cpr.FindByID(ctx, "plan-1"); err != nil {
		t.Fatalf("prime hit error: %v", err)
	}

	// Ensure we have a hit
	hits, misses, stales := cpr.Metrics()
	if hits != 1 {
		t.Fatalf("expected 1 hit, got hits=%d misses=%d stales=%d", hits, misses, stales)
	}

	// Mutate backend and invalidate
	backend.records["plan-1"].Name = "Updated"
	if err := cpr.Delete(ctx, "plan-1"); err != nil {
		t.Fatalf("delete error: %v", err)
	}

	// Simulate race: an in-flight request writes back stale data after Delete
	// We inject a stale envelope directly into the cache with an old timestamp
	staleEnv := cacheEnvelope{
		Data:     []byte(`{"id":"plan-1","name":"Original","amount":"1000","currency":"usd","interval":"month"}`),
		StoredAt: time.Now().Add(-time.Hour), // well before invalidation
	}
	if b, err := json.Marshal(staleEnv); err == nil {
		_ = mem.Set(ctx, cpr.cacheKey("plan-1"), b, time.Minute)
	}

	// The next read should detect the stale cached entry, count it, and refetch
	p, err := cpr.FindByID(ctx, "plan-1")
	if err != nil {
		t.Fatalf("read after stale injection error: %v", err)
	}
	if p.Name != "Updated" {
		t.Fatalf("expected Updated after stale detection, got %s", p.Name)
	}

	_, _, stalesAfter := cpr.Metrics()
	if stalesAfter < 1 {
		t.Fatalf("expected stale > 0 after stale read, got stales=%d", stalesAfter)
	}
}

func TestCachedPlanRepo_ListCaching(t *testing.T) {
	ctx := context.Background()
	backend := NewMockPlanRepo(
		&PlanRow{ID: "plan-a", Name: "A", Amount: "1000", Currency: "usd", Interval: "month"},
		&PlanRow{ID: "plan-b", Name: "B", Amount: "2000", Currency: "usd", Interval: "month"},
	)
	mem := cache.NewInMemory()
	cpr := NewCachedPlanRepo(backend, mem, time.Minute)

	// First list -> miss
	list1, err := cpr.List(ctx)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list1) != 2 {
		t.Fatalf("expected 2 plans, got %d", len(list1))
	}
	_, misses1, _ := cpr.Metrics()
	if misses1 == 0 {
		t.Fatalf("expected at least one miss for list")
	}

	// Second list -> hit
	list2, err := cpr.List(ctx)
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	if len(list2) != 2 {
		t.Fatalf("expected 2 plans on cached list, got %d", len(list2))
	}
	hits2, _, _ := cpr.Metrics()
	if hits2 == 0 {
		t.Fatalf("expected at least one hit for list")
	}

	// Invalidate via Delete should purge list cache
	backend.records["plan-a"].Name = "A-Updated"
	if err := cpr.Delete(ctx, "plan-a"); err != nil {
		t.Fatalf("delete error: %v", err)
	}

	list3, err := cpr.List(ctx)
	if err != nil {
		t.Fatalf("unexpected list error after invalidation: %v", err)
	}
	found := false
	for _, p := range list3 {
		if p.ID == "plan-a" && p.Name == "A-Updated" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected list to reflect updated plan-a after invalidation")
	}
}
