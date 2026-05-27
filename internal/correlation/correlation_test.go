package correlation_test

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/correlation"
)

func TestNewID_Format(t *testing.T) {
	id := correlation.NewID()
	require.NotEmpty(t, id)
	assert.Len(t, id, 36, "ID must be a standard UUID string")
}

func TestNewID_Uniqueness(t *testing.T) {
	const n = 10000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := correlation.NewID()
		_, dup := seen[id]
		assert.False(t, dup, "duplicate ID generated at iteration %d: %s", i, id)
		seen[id] = struct{}{}
	}
}

func TestNewID_NoPII(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := correlation.NewID()
		for pos, ch := range id {
			isHexDigit := (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f')
			isHyphen := ch == '-'
			assert.True(t, isHexDigit || isHyphen,
				"ID %q contains non-opaque character %q at position %d", id, ch, pos)
		}
	}
}

func TestNewID_ConcurrentGeneration(t *testing.T) {
	const goroutines = 50
	const idsPerGoroutine = 200

	var mu sync.Mutex
	seen := make(map[string]struct{}, goroutines*idsPerGoroutine)

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			local := make([]string, idsPerGoroutine)
			for j := 0; j < idsPerGoroutine; j++ {
				local[j] = correlation.NewID()
			}
			mu.Lock()
			for _, id := range local {
				_, dup := seen[id]
				assert.False(t, dup, "duplicate ID in concurrent test: %s", id)
				seen[id] = struct{}{}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
}

func TestWithRequestID_RoundTrip(t *testing.T) {
	id := correlation.NewID()
	ctx := correlation.WithRequestID(context.Background(), id)
	assert.Equal(t, id, correlation.RequestIDFromContext(ctx))
}

func TestRequestIDFromContext_EmptyWhenNotSet(t *testing.T) {
	assert.Empty(t, correlation.RequestIDFromContext(context.Background()))
}

func TestWithRequestID_OverridesParent(t *testing.T) {
	first := "first-id"
	second := "second-id"

	ctx := correlation.WithRequestID(context.Background(), first)
	ctx = correlation.WithRequestID(ctx, second)

	assert.Equal(t, second, correlation.RequestIDFromContext(ctx))
}

func TestWithRequestID_DoesNotMutateParent(t *testing.T) {
	base := context.Background()
	id := correlation.NewID()

	child := correlation.WithRequestID(base, id)
	_ = child

	assert.Empty(t, correlation.RequestIDFromContext(base))
}

func TestWithJobID_RoundTrip(t *testing.T) {
	id := correlation.NewID()
	ctx := correlation.WithJobID(context.Background(), id)
	assert.Equal(t, id, correlation.JobIDFromContext(ctx))
}

func TestJobIDFromContext_EmptyWhenNotSet(t *testing.T) {
	assert.Empty(t, correlation.JobIDFromContext(context.Background()))
}

func TestWithJobID_DoesNotMutateParent(t *testing.T) {
	base := context.Background()
	id := correlation.NewID()

	child := correlation.WithJobID(base, id)
	_ = child

	assert.Empty(t, correlation.JobIDFromContext(base))
}

func TestNoCollision_BothIDsInSameContext(t *testing.T) {
	reqID := "req-" + correlation.NewID()
	jobID := "job-" + correlation.NewID()

	ctx := correlation.WithRequestID(context.Background(), reqID)
	ctx = correlation.WithJobID(ctx, jobID)

	assert.Equal(t, reqID, correlation.RequestIDFromContext(ctx))
	assert.Equal(t, jobID, correlation.JobIDFromContext(ctx))
}

func TestNoCollision_OrderIndependent(t *testing.T) {
	reqID := correlation.NewID()
	jobID := correlation.NewID()

	ctx1 := correlation.WithJobID(context.Background(), jobID)
	ctx1 = correlation.WithRequestID(ctx1, reqID)

	ctx2 := correlation.WithRequestID(context.Background(), reqID)
	ctx2 = correlation.WithJobID(ctx2, jobID)

	assert.Equal(t, reqID, correlation.RequestIDFromContext(ctx1))
	assert.Equal(t, jobID, correlation.JobIDFromContext(ctx1))
	assert.Equal(t, reqID, correlation.RequestIDFromContext(ctx2))
	assert.Equal(t, jobID, correlation.JobIDFromContext(ctx2))
}

func TestRequestID_PropagatesAcrossGoroutines(t *testing.T) {
	id := correlation.NewID()
	ctx := correlation.WithRequestID(context.Background(), id)

	results := make(chan string, 10)
	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- correlation.RequestIDFromContext(ctx)
		}()
	}

	wg.Wait()
	close(results)

	for got := range results {
		assert.Equal(t, id, got)
	}
}

func TestJobID_PropagatesAcrossGoroutines(t *testing.T) {
	id := correlation.NewID()
	ctx := correlation.WithJobID(context.Background(), id)

	results := make(chan string, 5)
	var wg sync.WaitGroup

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- correlation.JobIDFromContext(ctx)
		}()
	}

	wg.Wait()
	close(results)

	for got := range results {
		assert.Equal(t, id, got)
	}
}

func TestBackgroundJob_HasJobIDWithoutRequestID(t *testing.T) {
	workerCtx := context.Background()

	jobID := correlation.NewID()
	ctx := correlation.WithJobID(workerCtx, jobID)

	assert.NotEmpty(t, correlation.JobIDFromContext(ctx))
	assert.Empty(t, correlation.RequestIDFromContext(ctx))
}