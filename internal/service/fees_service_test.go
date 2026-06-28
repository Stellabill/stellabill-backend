package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
