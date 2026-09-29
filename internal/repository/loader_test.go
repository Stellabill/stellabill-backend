package repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type trackingPlanRepo struct {
	MockPlanRepo
	findByIDsCount          uint64
	findByIDsAndTenantCount uint64
}

func (r *trackingPlanRepo) FindByIDs(ctx context.Context, ids []string) ([]*PlanRow, error) {
	atomic.AddUint64(&r.findByIDsCount, 1)
	return r.MockPlanRepo.FindByIDs(ctx, ids)
}

func (r *trackingPlanRepo) FindByIDsAndTenant(ctx context.Context, ids []string, tenantID string) ([]*PlanRow, error) {
	atomic.AddUint64(&r.findByIDsAndTenantCount, 1)
	return r.MockPlanRepo.FindByIDsAndTenant(ctx, ids, tenantID)
}

type trackingSubRepo struct {
	MockSubscriptionRepo
	findByIDsAndTenantCount uint64
	errToReturn             error
}

func (r *trackingSubRepo) FindByIDsAndTenant(ctx context.Context, ids []string, tenantID string) ([]*SubscriptionRow, error) {
	atomic.AddUint64(&r.findByIDsAndTenantCount, 1)
	if r.errToReturn != nil {
		return nil, r.errToReturn
	}
	return r.MockSubscriptionRepo.FindByIDsAndTenant(ctx, ids, tenantID)
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()
	if l := LoaderFromContext(ctx); l != nil {
		t.Fatalf("expected nil loader from empty context")
	}

	planRepo := NewMockPlanRepo()
	subRepo := NewMockSubscriptionRepo()
	loader := NewLoader(planRepo, subRepo)

	ctx = WithLoader(ctx, loader)
	if l := LoaderFromContext(ctx); l != loader {
		t.Fatalf("expected loader from context to match injected loader")
	}
}

func TestLoader_PlanBatching(t *testing.T) {
	p1 := &PlanRow{ID: "plan-1", TenantID: "t-1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "month"}
	p2 := &PlanRow{ID: "plan-2", TenantID: "t-1", Name: "Pro", Amount: "2000", Currency: "USD", Interval: "month"}
	p3 := &PlanRow{ID: "plan-3", TenantID: "t-1", Name: "Enterprise", Amount: "5000", Currency: "USD", Interval: "month"}

	baseRepo := NewMockPlanRepo(p1, p2, p3)
	trackRepo := &trackingPlanRepo{MockPlanRepo: *baseRepo}
	subRepo := NewMockSubscriptionRepo()

	loader := NewLoader(trackRepo, subRepo, WithBatchWait(10*time.Millisecond))

	ctx := context.Background()

	var wg sync.WaitGroup
	var r1, r2, r3 *PlanRow
	var err1, err2, err3 error

	wg.Add(3)
	go func() {
		defer wg.Done()
		r1, err1 = loader.LoadPlan(ctx, "t-1", "plan-1")
	}()
	go func() {
		defer wg.Done()
		r2, err2 = loader.LoadPlan(ctx, "t-1", "plan-2")
	}()
	go func() {
		defer wg.Done()
		r3, err3 = loader.LoadPlan(ctx, "t-1", "plan-3")
	}()

	wg.Wait()

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("unexpected error: %v %v %v", err1, err2, err3)
	}

	if r1.Name != "Basic" || r2.Name != "Pro" || r3.Name != "Enterprise" {
		t.Fatalf("unexpected plan names: %s, %s, %s", r1.Name, r2.Name, r3.Name)
	}

	if count := atomic.LoadUint64(&trackRepo.findByIDsAndTenantCount); count != 1 {
		t.Fatalf("expected exactly 1 batch query call, got %d", count)
	}
}

func TestLoader_SubscriptionBatching(t *testing.T) {
	s1 := &SubscriptionRow{ID: "sub-1", TenantID: "t-1", PlanID: "plan-1", Status: "active", Amount: "1000", Currency: "USD"}
	s2 := &SubscriptionRow{ID: "sub-2", TenantID: "t-1", PlanID: "plan-2", Status: "active", Amount: "2000", Currency: "USD"}

	baseSubRepo := NewMockSubscriptionRepo(s1, s2)
	trackSubRepo := &trackingSubRepo{MockSubscriptionRepo: *baseSubRepo}
	planRepo := NewMockPlanRepo()

	loader := NewLoader(planRepo, trackSubRepo, WithBatchWait(10*time.Millisecond))

	ctx := context.Background()

	var wg sync.WaitGroup
	var r1, r2 *SubscriptionRow
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		r1, err1 = loader.LoadSubscription(ctx, "t-1", "sub-1")
	}()
	go func() {
		defer wg.Done()
		r2, err2 = loader.LoadSubscription(ctx, "t-1", "sub-2")
	}()

	wg.Wait()

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected error: %v %v", err1, err2)
	}

	if r1.ID != "sub-1" || r2.ID != "sub-2" {
		t.Fatalf("unexpected sub IDs: %s, %s", r1.ID, r2.ID)
	}

	if count := atomic.LoadUint64(&trackSubRepo.findByIDsAndTenantCount); count != 1 {
		t.Fatalf("expected exactly 1 batch query call, got %d", count)
	}
}

func TestLoader_TenantIsolation(t *testing.T) {
	// Admin or multi-tenant scenario: Requests that mix tenants must NOT share loaders or batch queues.
	s1 := &SubscriptionRow{ID: "sub-1", TenantID: "tenant-A", Status: "active"}
	s2 := &SubscriptionRow{ID: "sub-2", TenantID: "tenant-B", Status: "active"}

	baseSubRepo := NewMockSubscriptionRepo(s1, s2)
	trackSubRepo := &trackingSubRepo{MockSubscriptionRepo: *baseSubRepo}
	planRepo := NewMockPlanRepo()

	loader := NewLoader(planRepo, trackSubRepo, WithBatchWait(10*time.Millisecond))

	ctx := context.Background()

	var wg sync.WaitGroup
	var r1, r2 *SubscriptionRow
	var err1, err2 error

	wg.Add(2)
	go func() {
		defer wg.Done()
		r1, err1 = loader.LoadSubscription(ctx, "tenant-A", "sub-1")
	}()
	go func() {
		defer wg.Done()
		r2, err2 = loader.LoadSubscription(ctx, "tenant-B", "sub-2")
	}()

	wg.Wait()

	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected error: %v %v", err1, err2)
	}

	if r1.TenantID != "tenant-A" || r2.TenantID != "tenant-B" {
		t.Fatalf("tenant isolation broken: r1.TenantID=%s, r2.TenantID=%s", r1.TenantID, r2.TenantID)
	}

	// Cross-tenant attempt: tenant-A loading sub-2 (belongs to tenant-B) should return ErrNotFound.
	_, errCross := loader.LoadSubscription(ctx, "tenant-A", "sub-2")
	if !errors.Is(errCross, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for cross-tenant load, got: %v", errCross)
	}

	if count := atomic.LoadUint64(&trackSubRepo.findByIDsAndTenantCount); count < 2 {
		t.Fatalf("expected at least 2 distinct tenant batch calls, got %d", count)
	}
}

func TestLoader_EmptyAndNotFoundIDs(t *testing.T) {
	planRepo := NewMockPlanRepo()
	subRepo := NewMockSubscriptionRepo()
	loader := NewLoader(planRepo, subRepo)

	ctx := context.Background()

	if _, err := loader.LoadPlan(ctx, "t-1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty plan ID, got %v", err)
	}

	if _, err := loader.LoadSubscription(ctx, "t-1", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for empty sub ID, got %v", err)
	}

	if _, err := loader.LoadPlan(ctx, "t-1", "non-existent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent plan ID, got %v", err)
	}

	if _, err := loader.LoadSubscription(ctx, "t-1", "non-existent"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound for non-existent sub ID, got %v", err)
	}
}

func TestLoader_MaxBatchAndDispatch(t *testing.T) {
	p1 := &PlanRow{ID: "plan-1", TenantID: "t-1", Name: "P1"}
	p2 := &PlanRow{ID: "plan-2", TenantID: "t-1", Name: "P2"}

	baseRepo := NewMockPlanRepo(p1, p2)
	trackRepo := &trackingPlanRepo{MockPlanRepo: *baseRepo}
	subRepo := NewMockSubscriptionRepo()

	// Set maxBatch = 2 so adding 2 items triggers immediate dispatch
	loader := NewLoader(trackRepo, subRepo, WithMaxBatch(2), WithBatchWait(1*time.Minute))

	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = loader.LoadPlan(ctx, "t-1", "plan-1")
	}()
	go func() {
		defer wg.Done()
		_, _ = loader.LoadPlan(ctx, "t-1", "plan-2")
	}()

	wg.Wait()

	if count := atomic.LoadUint64(&trackRepo.findByIDsAndTenantCount); count != 1 {
		t.Fatalf("expected immediate dispatch on maxBatch, got count %d", count)
	}

	// Test Manual Dispatch()
	loader.Dispatch()
}

func TestLoader_RepositoryErrorPropagation(t *testing.T) {
	planRepo := NewMockPlanRepo()
	expectedErr := errors.New("db connection failure")
	trackSubRepo := &trackingSubRepo{errToReturn: expectedErr}

	loader := NewLoader(planRepo, trackSubRepo, WithBatchWait(5*time.Millisecond))

	ctx := context.Background()
	_, err := loader.LoadSubscription(ctx, "t-1", "sub-1")
	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected %v, got %v", expectedErr, err)
	}
}

func TestLoader_ContextCancellation(t *testing.T) {
	planRepo := NewMockPlanRepo()
	subRepo := NewMockSubscriptionRepo()
	loader := NewLoader(planRepo, subRepo, WithBatchWait(1*time.Hour)) // long wait

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := loader.LoadPlan(ctx, "t-1", "plan-1")
	if err == nil {
		t.Fatalf("expected context error on cancelled context")
	}
}

func TestLoader_GlobalPlanLoader(t *testing.T) {
	p1 := &PlanRow{ID: "global-plan-1", TenantID: "", Name: "Global Plan"}
	baseRepo := NewMockPlanRepo(p1)
	trackRepo := &trackingPlanRepo{MockPlanRepo: *baseRepo}
	subRepo := NewMockSubscriptionRepo()

	loader := NewLoader(trackRepo, subRepo, WithBatchWait(5*time.Millisecond))

	ctx := context.Background()
	res, err := loader.LoadPlan(ctx, "", "global-plan-1")
	if err != nil || res.Name != "Global Plan" {
		t.Fatalf("unexpected global plan load result: res=%v, err=%v", res, err)
	}

	if count := atomic.LoadUint64(&trackRepo.findByIDsCount); count != 1 {
		t.Fatalf("expected FindByIDs count 1 for empty tenant plan load, got %d", count)
	}
}
