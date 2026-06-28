package worker

import (
	"context"
	"errors"
	"testing"
	"time"

	"stellarbill-backend/internal/middleware"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// MockIdempotencyStore allows injecting custom logic for testing the cleanup job.
type MockIdempotencyStore struct {
	CountExpiredPendingFunc func(ctx context.Context) (int64, error)
	DeleteExpiredBatchFunc  func(ctx context.Context, batchSize int) (int64, error)
}

func (m *MockIdempotencyStore) GetOrInsert(ctx context.Context, scope, key, method, path, payloadHash string, ttl time.Duration) (statusCode int, responseBody []byte, isReplay bool, isInFlight bool, err error) {
	return 0, nil, false, false, nil
}

func (m *MockIdempotencyStore) UpdateResponse(ctx context.Context, scope, key string, statusCode int, responseBody []byte) error {
	return nil
}

func (m *MockIdempotencyStore) Delete(ctx context.Context, scope, key string) error {
	return nil
}

func (m *MockIdempotencyStore) DeleteExpiredBatch(ctx context.Context, batchSize int) (int64, error) {
	if m.DeleteExpiredBatchFunc != nil {
		return m.DeleteExpiredBatchFunc(ctx, batchSize)
	}
	return 0, nil
}

func (m *MockIdempotencyStore) CountExpiredPending(ctx context.Context) (int64, error) {
	if m.CountExpiredPendingFunc != nil {
		return m.CountExpiredPendingFunc(ctx)
	}
	return 0, nil
}

func TestIdempotencyCleanupJob_Run(t *testing.T) {
	tests := []struct {
		name                string
		setupMock           func() *MockIdempotencyStore
		setupCtx            func() (context.Context, context.CancelFunc)
		expectedPurgedDelta float64
		expectedPending     float64
	}{
		{
			name: "Success / Normal Completion",
			setupMock: func() *MockIdempotencyStore {
				callCount := 0
				return &MockIdempotencyStore{
					CountExpiredPendingFunc: func(ctx context.Context) (int64, error) {
						return 150, nil
					},
					DeleteExpiredBatchFunc: func(ctx context.Context, batchSize int) (int64, error) {
						callCount++
						if callCount == 1 {
							return 5000, nil
						}
						// Return 0 on the second call to gracefully break the loop
						return 0, nil
					},
				}
			},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			expectedPurgedDelta: 5000,
			expectedPending:     150,
		},
		{
			name: "Budget Window Exceeded",
			setupMock: func() *MockIdempotencyStore {
				return &MockIdempotencyStore{
					CountExpiredPendingFunc: func(ctx context.Context) (int64, error) {
						return 200, nil
					},
					DeleteExpiredBatchFunc: func(ctx context.Context, batchSize int) (int64, error) {
						// Block until the context timeout fires, then return an error
						<-ctx.Done()
						return 0, ctx.Err()
					},
				}
			},
			setupCtx: func() (context.Context, context.CancelFunc) {
				// Use a tight timeout (e.g. 5ms) to simulate hitting the budget instantly.
				// Since child contexts cannot exceed parent timeouts, the Run method's 
				// 5-minute internal context will cap out immediately.
				return context.WithTimeout(context.Background(), 5*time.Millisecond)
			},
			expectedPurgedDelta: 0,
			expectedPending:     200,
		},
		{
			name: "Database Error Handling",
			setupMock: func() *MockIdempotencyStore {
				return &MockIdempotencyStore{
					CountExpiredPendingFunc: func(ctx context.Context) (int64, error) {
						return 50, nil
					},
					DeleteExpiredBatchFunc: func(ctx context.Context, batchSize int) (int64, error) {
						// Return a database error immediately
						return 0, errors.New("database connection failed")
					},
				}
			},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			expectedPurgedDelta: 0,
			expectedPending:     50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Record the counter value prior to execution to compute the delta,
			// as counters persist across tests in the global Prometheus registry.
			purgedBefore := testutil.ToFloat64(middleware.IdempotencyKeysPurgedTotal)

			mockStore := tt.setupMock()
			job := NewIdempotencyCleanupJob(mockStore)

			ctx, cancel := tt.setupCtx()
			defer cancel()

			// Execute the job
			job.Run(ctx)

			purgedAfter := testutil.ToFloat64(middleware.IdempotencyKeysPurgedTotal)
			pendingValue := testutil.ToFloat64(middleware.IdempotencyKeysExpiredPending)

			actualDelta := purgedAfter - purgedBefore
			if actualDelta != tt.expectedPurgedDelta {
				t.Errorf("Expected purged delta %v, got %v", tt.expectedPurgedDelta, actualDelta)
			}

			if pendingValue != tt.expectedPending {
				t.Errorf("Expected pending gauge %v, got %v", tt.expectedPending, pendingValue)
			}
		})
	}
}
