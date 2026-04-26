package repository

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"stellarbill-backend/internal/cache"
)

func TestCachedSubscriptionRepo_FindByID_HitMissAndStale(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-1", PlanID: "plan-1", TenantID: "tenant-a",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// First read -> miss
	sr, err := csr.FindByID(ctx, "sub-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sr.Status != "active" {
		t.Fatalf("expected active, got %s", sr.Status)
	}
	_, misses, _ := csr.Metrics()
	if misses == 0 {
		t.Fatalf("expected at least one miss")
	}

	// Second read -> hit
	sr2, err := csr.FindByID(ctx, "sub-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sr2.Status != "active" {
		t.Fatalf("expected active on cached read, got %s", sr2.Status)
	}
	hits, _, _ := csr.Metrics()
	if hits == 0 {
		t.Fatalf("expected at least one hit")
	}

	// Mutate backend and invalidate
	backend.records["sub-1"].Status = "canceled"
	if err := csr.Delete(ctx, "sub-1", "tenant-a"); err != nil {
		t.Fatalf("invalidate error: %v", err)
	}

	// Simulate race: in-flight request writes back stale data after Delete
	staleEnv := cacheEnvelope{
		Data:     []byte(`{"id":"sub-1","plan_id":"plan-1","tenant_id":"tenant-a","status":"active","amount":"1000","currency":"usd","interval":"month"}`),
		StoredAt: time.Now().Add(-time.Hour),
	}
	if b, err := json.Marshal(staleEnv); err == nil {
		_ = mem.Set(ctx, csr.cacheKey("sub-1"), b, time.Minute)
	}

	// Next read should detect stale entry, count it, and refetch
	sr3, err := csr.FindByID(ctx, "sub-1")
	if err != nil {
		t.Fatalf("read after stale injection error: %v", err)
	}
	if sr3.Status != "canceled" {
		t.Fatalf("expected canceled after stale detection, got %s", sr3.Status)
	}
	_, _, stales := csr.Metrics()
	if stales < 1 {
		t.Fatalf("expected stale > 0 after stale read, got stales=%d", stales)
	}
}

func TestCachedSubscriptionRepo_FindByIDAndTenant_HitMissAndStale(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-2", PlanID: "plan-1", TenantID: "tenant-b",
		Status: "active", Amount: "2000", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// First read -> miss
	sr, err := csr.FindByIDAndTenant(ctx, "sub-2", "tenant-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sr.TenantID != "tenant-b" {
		t.Fatalf("expected tenant-b, got %s", sr.TenantID)
	}
	_, misses, _ := csr.Metrics()
	if misses == 0 {
		t.Fatalf("expected at least one miss")
	}

	// Second read -> hit
	sr2, err := csr.FindByIDAndTenant(ctx, "sub-2", "tenant-b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sr2.TenantID != "tenant-b" {
		t.Fatalf("expected tenant-b on cached read, got %s", sr2.TenantID)
	}
	hits, _, _ := csr.Metrics()
	if hits == 0 {
		t.Fatalf("expected at least one hit")
	}

	// Mutate backend and invalidate
	backend.records["sub-2"].Status = "past_due"
	if err := csr.Delete(ctx, "sub-2", "tenant-b"); err != nil {
		t.Fatalf("invalidate error: %v", err)
	}

	// Simulate race: in-flight request writes back stale data after Delete
	staleEnv := cacheEnvelope{
		Data:     []byte(`{"id":"sub-2","plan_id":"plan-1","tenant_id":"tenant-b","status":"active","amount":"2000","currency":"usd","interval":"month"}`),
		StoredAt: time.Now().Add(-time.Hour),
	}
	if b, err := json.Marshal(staleEnv); err == nil {
		_ = mem.Set(ctx, csr.tenantCacheKey("sub-2", "tenant-b"), b, time.Minute)
	}

	// Next read should detect stale entry, count it, and refetch
	sr3, err := csr.FindByIDAndTenant(ctx, "sub-2", "tenant-b")
	if err != nil {
		t.Fatalf("read after stale injection error: %v", err)
	}
	if sr3.Status != "past_due" {
		t.Fatalf("expected past_due after stale detection, got %s", sr3.Status)
	}
	_, _, stales := csr.Metrics()
	if stales < 1 {
		t.Fatalf("expected stale > 0 after stale read, got stales=%d", stales)
	}
}

func TestCachedSubscriptionRepo_FindByIDAndTenant_WrongTenant(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-3", PlanID: "plan-1", TenantID: "tenant-c",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// Find with correct tenant should work and cache
	_, err := csr.FindByIDAndTenant(ctx, "sub-3", "tenant-c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find with wrong tenant should fail even if cache has the entry
	_, err = csr.FindByIDAndTenant(ctx, "sub-3", "tenant-x")
	if err == nil {
		t.Fatalf("expected error for wrong tenant")
	}
}

func TestCachedSubscriptionRepo_CacheOutageFallback(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-4", PlanID: "plan-1", TenantID: "tenant-d",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	fc := &faultyCache{}
	csr := NewCachedSubscriptionRepo(backend, fc, time.Minute)

	sr, err := csr.FindByID(ctx, "sub-4")
	if err != nil {
		t.Fatalf("expected fallback to backend, got error: %v", err)
	}
	if sr.ID != "sub-4" {
		t.Fatalf("expected sub-4, got %s", sr.ID)
	}
}

func TestCachedSubscriptionRepo_ConcurrentInvalidation(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-5", PlanID: "plan-1", TenantID: "tenant-e",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// Prime cache via both keys
	if _, err := csr.FindByID(ctx, "sub-5"); err != nil {
		t.Fatalf("prime error: %v", err)
	}
	if _, err := csr.FindByIDAndTenant(ctx, "sub-5", "tenant-e"); err != nil {
		t.Fatalf("prime tenant error: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, err := csr.FindByID(ctx, "sub-5")
				if err != nil {
					t.Errorf("reader error: %v", err)
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}

	// Invalidate while readers are running
	time.Sleep(5 * time.Millisecond)
	backend.records["sub-5"].Status = "canceled"
	if err := csr.Delete(ctx, "sub-5", "tenant-e"); err != nil {
		t.Fatalf("invalidate error: %v", err)
	}

	wg.Wait()

	// After invalidation, next read should observe updated value
	sr, err := csr.FindByID(ctx, "sub-5")
	if err != nil {
		t.Fatalf("final read error: %v", err)
	}
	if sr.Status != "canceled" {
		t.Fatalf("expected canceled after invalidation, got %s", sr.Status)
	}
}

func TestCachedSubscriptionRepo_CorruptEnvelope(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-corrupt", PlanID: "plan-1", TenantID: "tenant-c",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// Inject valid envelope with corrupt inner data
	env := cacheEnvelope{Data: []byte("not-json"), StoredAt: time.Now()}
	if b, err := json.Marshal(env); err == nil {
		_ = mem.Set(ctx, csr.cacheKey("sub-corrupt"), b, time.Minute)
	}

	sr, err := csr.FindByID(ctx, "sub-corrupt")
	if err != nil {
		t.Fatalf("unexpected error on corrupt envelope: %v", err)
	}
	if sr.ID != "sub-corrupt" {
		t.Fatalf("expected fallback to backend on corrupt envelope, got %s", sr.ID)
	}
}

func TestCachedSubscriptionRepo_StaleTenant(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-stale", PlanID: "plan-1", TenantID: "tenant-d",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// Prime tenant cache
	if _, err := csr.FindByIDAndTenant(ctx, "sub-stale", "tenant-d"); err != nil {
		t.Fatalf("prime error: %v", err)
	}

	// Mutate backend
	backend.records["sub-stale"].Status = "canceled"

	// Delete to invalidate
	if err := csr.Delete(ctx, "sub-stale", "tenant-d"); err != nil {
		t.Fatalf("delete error: %v", err)
	}

	// Inject stale tenant envelope directly
	staleEnv := cacheEnvelope{
		Data:     []byte(`{"id":"sub-stale","plan_id":"plan-1","tenant_id":"tenant-d","status":"active","amount":"1000","currency":"usd","interval":"month"}`),
		StoredAt: time.Now().Add(-time.Hour),
	}
	if b, err := json.Marshal(staleEnv); err == nil {
		_ = mem.Set(ctx, csr.tenantCacheKey("sub-stale", "tenant-d"), b, time.Minute)
	}

	// Should detect stale tenant entry and refetch
	sr, err := csr.FindByIDAndTenant(ctx, "sub-stale", "tenant-d")
	if err != nil {
		t.Fatalf("tenant read after stale injection error: %v", err)
	}
	if sr.Status != "canceled" {
		t.Fatalf("expected canceled after stale detection, got %s", sr.Status)
	}

	_, _, stales := csr.Metrics()
	if stales < 1 {
		t.Fatalf("expected stale > 0 for tenant, got stales=%d", stales)
	}
}

func TestCachedSubscriptionRepo_DeleteNilCache(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-nil", PlanID: "plan-1", TenantID: "tenant-e",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	csr := NewCachedSubscriptionRepo(backend, nil, time.Minute)

	if err := csr.Delete(ctx, "sub-nil", "tenant-e"); err != nil {
		t.Fatalf("unexpected error on nil cache delete: %v", err)
	}
}

func TestCachedSubscriptionRepo_CacheOutageFallback_Stale(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-faulty", PlanID: "plan-1", TenantID: "tenant-f",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	fc := &faultyCache{}
	csr := NewCachedSubscriptionRepo(backend, fc, time.Minute)

	// Faulty cache returns errors; should fallback to backend
	sr, err := csr.FindByIDAndTenant(ctx, "sub-faulty", "tenant-f")
	if err != nil {
		t.Fatalf("expected fallback to backend, got error: %v", err)
	}
	if sr.ID != "sub-faulty" {
		t.Fatalf("expected sub-faulty, got %s", sr.ID)
	}
}

func TestCachedSubscriptionRepo_InvalidateClearsBothKeys(t *testing.T) {
	ctx := context.Background()
	backend := NewMockSubscriptionRepo(&SubscriptionRow{
		ID: "sub-6", PlanID: "plan-1", TenantID: "tenant-f",
		Status: "active", Amount: "1000", Currency: "usd", Interval: "month",
	})
	mem := cache.NewInMemory()
	csr := NewCachedSubscriptionRepo(backend, mem, time.Minute)

	// Prime both keys
	if _, err := csr.FindByID(ctx, "sub-6"); err != nil {
		t.Fatalf("prime error: %v", err)
	}
	if _, err := csr.FindByIDAndTenant(ctx, "sub-6", "tenant-f"); err != nil {
		t.Fatalf("prime tenant error: %v", err)
	}

	// Invalidate
	if err := csr.Delete(ctx, "sub-6", "tenant-f"); err != nil {
		t.Fatalf("invalidate error: %v", err)
	}

	// Mutate backend
	backend.records["sub-6"].Status = "past_due"

	// Both reads should miss cache and return updated value
	sr1, err := csr.FindByID(ctx, "sub-6")
	if err != nil {
		t.Fatalf("findbyid after invalidate error: %v", err)
	}
	if sr1.Status != "past_due" {
		t.Fatalf("expected past_due from FindByID, got %s", sr1.Status)
	}

	sr2, err := csr.FindByIDAndTenant(ctx, "sub-6", "tenant-f")
	if err != nil {
		t.Fatalf("findbyidandtenant after invalidate error: %v", err)
	}
	if sr2.Status != "past_due" {
		t.Fatalf("expected past_due from FindByIDAndTenant, got %s", sr2.Status)
	}
}
