package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubFreshness is a configurable FreshnessProvider for WithFreshness tests.
type stubFreshness struct {
	stale         bool
	lastRefreshed time.Time
	never         bool
	err           error
}

func (s stubFreshness) IsStale(_ context.Context, _ time.Time) (bool, time.Time, bool, error) {
	return s.stale, s.lastRefreshed, s.never, s.err
}

func TestWithFreshness_Fresh(t *testing.T) {
	refreshed := time.Now().UTC().Add(-time.Minute)
	h := &FeeHistory{}
	err := h.WithFreshness(context.Background(), stubFreshness{stale: false, lastRefreshed: refreshed}, time.Now())
	require.NoError(t, err)
	require.NotNil(t, h.LastRefreshedAt)
	assert.Equal(t, refreshed, *h.LastRefreshedAt)
	assert.False(t, h.Stale)
}

func TestWithFreshness_StaleButServed(t *testing.T) {
	refreshed := time.Now().UTC().Add(-3 * time.Hour)
	h := &FeeHistory{}
	err := h.WithFreshness(context.Background(), stubFreshness{stale: true, lastRefreshed: refreshed}, time.Now())
	require.NoError(t, err)
	assert.True(t, h.Stale)
	require.NotNil(t, h.LastRefreshedAt)
}

func TestWithFreshness_NeverRefreshed(t *testing.T) {
	h := &FeeHistory{}
	err := h.WithFreshness(context.Background(), stubFreshness{stale: true, never: true}, time.Now())
	require.NoError(t, err)
	assert.Nil(t, h.LastRefreshedAt)
	assert.True(t, h.Stale)
}

func TestWithFreshness_ProviderError(t *testing.T) {
	h := &FeeHistory{}
	err := h.WithFreshness(context.Background(), stubFreshness{err: errors.New("boom")}, time.Now())
	require.Error(t, err)
}

func TestWithFreshness_NilProvider(t *testing.T) {
	h := &FeeHistory{}
	// A nil provider is a no-op (raw-data path) and must not panic.
	require.NoError(t, h.WithFreshness(context.Background(), nil, time.Now()))
	assert.Nil(t, h.LastRefreshedAt)
	assert.False(t, h.Stale)
}

func TestWithFreshness_NilReceiver(t *testing.T) {
	var h *FeeHistory
	require.NoError(t, h.WithFreshness(context.Background(), stubFreshness{}, time.Now()))
}

func TestGetFeeHistory_DefaultRange(t *testing.T) {
	svc := NewFeeService()
	now := time.Now().UTC()
	history, err := svc.GetFeeHistory("", now.AddDate(0, -2, 0), now)
	require.NoError(t, err)
	assert.NotNil(t, history)
	assert.NotEmpty(t, history.Records)
	assert.NotEmpty(t, history.Trends)
}

func TestGetFeeHistory_FilterByType(t *testing.T) {
	svc := NewFeeService()
	now := time.Now().UTC()
	history, err := svc.GetFeeHistory("transaction", now.AddDate(0, -2, 0), now)
	require.NoError(t, err)
	for _, r := range history.Records {
		assert.Equal(t, "transaction", r.Type)
	}
}

func TestGetFeeHistory_EmptyRange(t *testing.T) {
	svc := NewFeeService()
	future := time.Now().UTC().AddDate(1, 0, 0)
	history, err := svc.GetFeeHistory("", future, future.AddDate(0, 1, 0))
	require.NoError(t, err)
	assert.Empty(t, history.Records)
	assert.Empty(t, history.Trends)
}

func TestGetFeeHistory_TrendChangePercent(t *testing.T) {
	svc := NewFeeService()
	now := time.Now().UTC()
	history, err := svc.GetFeeHistory("transaction", now.AddDate(0, -2, 0), now)
	require.NoError(t, err)
	for _, trend := range history.Trends {
		assert.Equal(t, "transaction", trend.Type)
		assert.Greater(t, trend.TotalAmount, 0.0)
		assert.Greater(t, trend.Count, 0)
	}
}
