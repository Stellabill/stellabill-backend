package fees

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryService_ListFees(t *testing.T) {
	svc := NewMemoryService()
	ctx := context.Background()

	t.Run("returns fees for subscription", func(t *testing.T) {
		result, err := svc.ListFees(ctx, "sub-123", "", "")
		require.NoError(t, err)
		assert.Len(t, result, 3)
		for _, f := range result {
			assert.Equal(t, "sub-123", f.SubscriptionID)
		}
	})

	t.Run("filters by status", func(t *testing.T) {
		result, err := svc.ListFees(ctx, "sub-123", "paid", "")
		require.NoError(t, err)
		assert.Len(t, result, 2)
		for _, f := range result {
			assert.Equal(t, "paid", f.Status)
		}
	})

	t.Run("filters by kind", func(t *testing.T) {
		result, err := svc.ListFees(ctx, "sub-123", "", "charge")
		require.NoError(t, err)
		assert.Len(t, result, 3)
	})

	t.Run("returns empty for unknown subscription", func(t *testing.T) {
		result, err := svc.ListFees(ctx, "sub-unknown", "", "")
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestMemoryService_GetHistory(t *testing.T) {
	svc := NewMemoryService()
	ctx := context.Background()

	t.Run("returns history ordered desc", func(t *testing.T) {
		result, err := svc.GetHistory(ctx, "sub-123", 0)
		require.NoError(t, err)
		assert.Len(t, result, 3)
		// Verify descending order
		for i := 1; i < len(result); i++ {
			assert.True(t, result[i-1].CreatedAt.After(result[i].CreatedAt) || result[i-1].CreatedAt.Equal(result[i].CreatedAt))
		}
	})

	t.Run("respects limit", func(t *testing.T) {
		result, err := svc.GetHistory(ctx, "sub-123", 2)
		require.NoError(t, err)
		assert.Len(t, result, 2)
	})

	t.Run("returns empty for unknown subscription", func(t *testing.T) {
		result, err := svc.GetHistory(ctx, "sub-unknown", 0)
		require.NoError(t, err)
		assert.Empty(t, result)
	})
}

func TestMemoryService_GetTrends(t *testing.T) {
	svc := NewMemoryService()
	ctx := context.Background()

	t.Run("returns trend with points", func(t *testing.T) {
		trend, err := svc.GetTrends(ctx, "sub-123")
		require.NoError(t, err)
		require.NotNil(t, trend)
		assert.Equal(t, "sub-123", trend.SubscriptionID)
		assert.Equal(t, "USD", trend.Currency)
		assert.NotEmpty(t, trend.Points)
		assert.Greater(t, trend.TotalCents, int64(0))
		assert.Greater(t, trend.AvgCents, int64(0))
	})

	t.Run("points are sorted ascending by period", func(t *testing.T) {
		trend, err := svc.GetTrends(ctx, "sub-123")
		require.NoError(t, err)
		for i := 1; i < len(trend.Points); i++ {
			assert.LessOrEqual(t, trend.Points[i-1].Period, trend.Points[i].Period)
		}
	})

	t.Run("returns empty trend for unknown subscription", func(t *testing.T) {
		trend, err := svc.GetTrends(ctx, "sub-unknown")
		require.NoError(t, err)
		require.NotNil(t, trend)
		assert.Empty(t, trend.Points)
		assert.Equal(t, int64(0), trend.TotalCents)
		assert.Equal(t, int64(0), trend.AvgCents)
	})
}
