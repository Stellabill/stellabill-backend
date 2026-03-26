package repository

import (
    "context"
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

    hits, misses := cpr.Metrics()
    if misses == 0 {
        t.Fatalf("expected at least one miss, got hits=%d misses=%d", hits, misses)
    }

    // Second read -> should hit cache
    p2, err := cpr.FindByID(ctx, "plan-1")
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if p2.Name != "Original" {
        t.Fatalf("expected Original on cached read, got %s", p2.Name)
    }

    h2, m2 := cpr.Metrics()
    if h2 == 0 {
        t.Fatalf("expected hit > 0 after repeated read, got hits=%d misses=%d", h2, m2)
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

func (f *faultyCache) Get(_ context.Context, _ string) ([]byte, error) { return nil, errors.New("cache down") }
func (f *faultyCache) Set(_ context.Context, _ string, _ []byte, _ time.Duration) error { return errors.New("cache down") }
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
    if _, err := cpr.FindByID(ctx, "plan-3"); err != nil { t.Fatalf("prime error: %v", err) }

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
    if err != nil { t.Fatalf("final read error: %v", err) }
    if p.Name != "C2" {
        t.Fatalf("expected C2 after invalidation, got %s", p.Name)
    }
}
