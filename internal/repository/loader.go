package repository

import (
	"context"
	"sync"
	"time"
)

type loaderContextKey string

const loaderKey loaderContextKey = "stellabill.repository.loader"

// WithLoader returns a new context carrying the given Loader.
func WithLoader(ctx context.Context, l *Loader) context.Context {
	return context.WithValue(ctx, loaderKey, l)
}

// LoaderFromContext retrieves the Loader from the context, or nil if none exists.
func LoaderFromContext(ctx context.Context) *Loader {
	if l, ok := ctx.Value(loaderKey).(*Loader); ok {
		return l
	}
	return nil
}

// Loader manages request-scoped, tenant-isolated batching for Plan and Subscription lookups.
type Loader struct {
	planRepo PlanRepository
	subRepo  SubscriptionRepository

	batchWait time.Duration
	maxBatch  int

	mu          sync.Mutex
	planLoaders map[string]*tenantPlanLoader
	subLoaders  map[string]*tenantSubLoader
}

// Option configures Loader options.
type Option func(*Loader)

// WithBatchWait sets the wait duration before auto-dispatching queued batch items.
func WithBatchWait(d time.Duration) Option {
	return func(l *Loader) {
		l.batchWait = d
	}
}

// WithMaxBatch sets the maximum batch size before auto-dispatching.
func WithMaxBatch(size int) Option {
	return func(l *Loader) {
		l.maxBatch = size
	}
}

// NewLoader creates a new Loader instance with tenant-isolated batching queues.
func NewLoader(planRepo PlanRepository, subRepo SubscriptionRepository, opts ...Option) *Loader {
	l := &Loader{
		planRepo:    planRepo,
		subRepo:     subRepo,
		batchWait:   2 * time.Millisecond,
		maxBatch:    100,
		planLoaders: make(map[string]*tenantPlanLoader),
		subLoaders:  make(map[string]*tenantSubLoader),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// LoadPlan fetches a Plan by ID for a given tenant. Lookups for the same tenant are coalesced.
func (l *Loader) LoadPlan(ctx context.Context, tenantID string, id string) (*PlanRow, error) {
	if id == "" {
		return nil, ErrNotFound
	}

	tpl := l.getOrCreatePlanLoader(tenantID)
	return tpl.load(ctx, id)
}

// LoadSubscription fetches a Subscription by ID for a given tenant. Lookups for the same tenant are coalesced.
func (l *Loader) LoadSubscription(ctx context.Context, tenantID string, id string) (*SubscriptionRow, error) {
	if id == "" {
		return nil, ErrNotFound
	}

	tsl := l.getOrCreateSubLoader(tenantID)
	return tsl.load(ctx, id)
}

// Dispatch immediately executes all pending batch queries across all tenant loaders.
func (l *Loader) Dispatch() {
	l.mu.Lock()
	var planLoaders []*tenantPlanLoader
	for _, pl := range l.planLoaders {
		planLoaders = append(planLoaders, pl)
	}
	var subLoaders []*tenantSubLoader
	for _, sl := range l.subLoaders {
		subLoaders = append(subLoaders, sl)
	}
	l.mu.Unlock()

	for _, pl := range planLoaders {
		pl.dispatch()
	}
	for _, sl := range subLoaders {
		sl.dispatch()
	}
}

func (l *Loader) getOrCreatePlanLoader(tenantID string) *tenantPlanLoader {
	l.mu.Lock()
	defer l.mu.Unlock()

	if pl, ok := l.planLoaders[tenantID]; ok {
		return pl
	}
	pl := &tenantPlanLoader{
		loader:   l,
		tenantID: tenantID,
		pending:  make(map[string][]chan planResult),
	}
	l.planLoaders[tenantID] = pl
	return pl
}

func (l *Loader) getOrCreateSubLoader(tenantID string) *tenantSubLoader {
	l.mu.Lock()
	defer l.mu.Unlock()

	if sl, ok := l.subLoaders[tenantID]; ok {
		return sl
	}
	sl := &tenantSubLoader{
		loader:   l,
		tenantID: tenantID,
		pending:  make(map[string][]chan subResult),
	}
	l.subLoaders[tenantID] = sl
	return sl
}

type planResult struct {
	row *PlanRow
	err error
}

type tenantPlanLoader struct {
	loader   *Loader
	tenantID string

	mu      sync.Mutex
	pending map[string][]chan planResult
	timer   *time.Timer
}

func (tpl *tenantPlanLoader) load(ctx context.Context, id string) (*PlanRow, error) {
	ch := make(chan planResult, 1)

	tpl.mu.Lock()
	tpl.pending[id] = append(tpl.pending[id], ch)
	count := len(tpl.pending)

	if count == 1 {
		tpl.timer = time.AfterFunc(tpl.loader.batchWait, func() {
			tpl.dispatch()
		})
	}
	shouldDispatch := count >= tpl.loader.maxBatch
	tpl.mu.Unlock()

	if shouldDispatch {
		tpl.dispatch()
	}

	select {
	case res := <-ch:
		return res.row, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (tpl *tenantPlanLoader) dispatch() {
	tpl.mu.Lock()
	if len(tpl.pending) == 0 {
		tpl.mu.Unlock()
		return
	}
	if tpl.timer != nil {
		tpl.timer.Stop()
		tpl.timer = nil
	}
	pending := tpl.pending
	tpl.pending = make(map[string][]chan planResult)
	tpl.mu.Unlock()

	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}

	var rows []*PlanRow
	var err error
	if tpl.tenantID != "" {
		rows, err = tpl.loader.planRepo.FindByIDsAndTenant(context.Background(), ids, tpl.tenantID)
	} else {
		rows, err = tpl.loader.planRepo.FindByIDs(context.Background(), ids)
	}

	rowMap := make(map[string]*PlanRow, len(rows))
	if err == nil {
		for _, r := range rows {
			rowMap[r.ID] = r
		}
	}

	for id, chs := range pending {
		var res planResult
		if err != nil {
			res = planResult{err: err}
		} else if r, ok := rowMap[id]; ok {
			res = planResult{row: r}
		} else {
			res = planResult{err: ErrNotFound}
		}
		for _, ch := range chs {
			ch <- res
		}
	}
}

type subResult struct {
	row *SubscriptionRow
	err error
}

type tenantSubLoader struct {
	loader   *Loader
	tenantID string

	mu      sync.Mutex
	pending map[string][]chan subResult
	timer   *time.Timer
}

func (tsl *tenantSubLoader) load(ctx context.Context, id string) (*SubscriptionRow, error) {
	ch := make(chan subResult, 1)

	tsl.mu.Lock()
	tsl.pending[id] = append(tsl.pending[id], ch)
	count := len(tsl.pending)

	if count == 1 {
		tsl.timer = time.AfterFunc(tsl.loader.batchWait, func() {
			tsl.dispatch()
		})
	}
	shouldDispatch := count >= tsl.loader.maxBatch
	tsl.mu.Unlock()

	if shouldDispatch {
		tsl.dispatch()
	}

	select {
	case res := <-ch:
		return res.row, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (tsl *tenantSubLoader) dispatch() {
	tsl.mu.Lock()
	if len(tsl.pending) == 0 {
		tsl.mu.Unlock()
		return
	}
	if tsl.timer != nil {
		tsl.timer.Stop()
		tsl.timer = nil
	}
	pending := tsl.pending
	tsl.pending = make(map[string][]chan subResult)
	tsl.mu.Unlock()

	ids := make([]string, 0, len(pending))
	for id := range pending {
		ids = append(ids, id)
	}

	rows, err := tsl.loader.subRepo.FindByIDsAndTenant(context.Background(), ids, tsl.tenantID)

	rowMap := make(map[string]*SubscriptionRow, len(rows))
	if err == nil {
		for _, r := range rows {
			rowMap[r.ID] = r
		}
	}

	for id, chs := range pending {
		var res subResult
		if err != nil {
			res = subResult{err: err}
		} else if r, ok := rowMap[id]; ok {
			res = subResult{row: r}
		} else {
			res = subResult{err: ErrNotFound}
		}
		for _, ch := range chs {
			ch <- res
		}
	}
}
