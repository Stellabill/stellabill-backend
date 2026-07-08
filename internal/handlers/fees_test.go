package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/service"
)

// mockFeeService implements service.FeeService for tests.
type mockFeeService struct {
	history *service.FeeHistory
	err     error
}

func (m *mockFeeService) GetFeeHistory(_ string, _, _ time.Time) (*service.FeeHistory, error) {
	return m.history, m.err
}

// mockFreshness implements service.FreshnessProvider for tests.
type mockFreshness struct {
	stale         bool
	lastRefreshed time.Time
	never         bool
	err           error
}

func (m *mockFreshness) IsStale(_ context.Context, _ time.Time) (bool, time.Time, bool, error) {
	return m.stale, m.lastRefreshed, m.never, m.err
}

// newHistoryRequest builds a GET /api/v1/fees/history test context.
func newHistoryRequest() (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/fees/history", nil)
	return w, c
}

func TestGetFeeHistory_FreshnessFresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	refreshed := time.Now().UTC().Add(-10 * time.Minute)
	h := NewFeesHandler(
		&mockFeeService{history: &service.FeeHistory{Records: []service.FeeRecord{}, Trends: []service.FeeTrend{}}},
		&mockFreshness{stale: false, lastRefreshed: refreshed},
	)

	w, c := newHistoryRequest()
	h.GetFeeHistory(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp service.FeeHistory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.LastRefreshedAt)
	assert.WithinDuration(t, refreshed, *resp.LastRefreshedAt, time.Second)
	assert.False(t, resp.Stale)
}

func TestGetFeeHistory_FreshnessStaleButServed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	refreshed := time.Now().UTC().Add(-5 * time.Hour)
	h := NewFeesHandler(
		&mockFeeService{history: &service.FeeHistory{Records: []service.FeeRecord{}, Trends: []service.FeeTrend{}}},
		&mockFreshness{stale: true, lastRefreshed: refreshed},
	)

	w, c := newHistoryRequest()
	h.GetFeeHistory(c)

	// Stale data is still served (200), just flagged.
	require.Equal(t, http.StatusOK, w.Code)
	var resp service.FeeHistory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Stale)
	require.NotNil(t, resp.LastRefreshedAt)
}

func TestGetFeeHistory_FreshnessNeverRefreshed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(
		&mockFeeService{history: &service.FeeHistory{Records: []service.FeeRecord{}, Trends: []service.FeeTrend{}}},
		&mockFreshness{stale: true, never: true},
	)

	w, c := newHistoryRequest()
	h.GetFeeHistory(c)

	require.Equal(t, http.StatusOK, w.Code)
	var resp service.FeeHistory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp.LastRefreshedAt, "never-refreshed view must not report a timestamp")
	assert.True(t, resp.Stale)
}

func TestGetFeeHistory_FreshnessErrorStillServes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(
		&mockFeeService{history: &service.FeeHistory{Records: []service.FeeRecord{}, Trends: []service.FeeTrend{}}},
		&mockFreshness{err: errors.New("freshness lookup failed")},
	)

	w, c := newHistoryRequest()
	h.GetFeeHistory(c)

	// A freshness lookup error must not fail the report.
	require.Equal(t, http.StatusOK, w.Code)
	var resp service.FeeHistory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Nil(t, resp.LastRefreshedAt)
	assert.False(t, resp.Stale)
}

func TestGetFeeHistory_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(&mockFeeService{
		history: &service.FeeHistory{
			Records: []service.FeeRecord{{ID: "fee-1", Type: "transaction", Amount: 1.5, Currency: "USD", CreatedAt: time.Now()}},
			Trends:  []service.FeeTrend{{Type: "transaction", Count: 1, TotalAmount: 1.5}},
		},
	}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/fees/history", nil)

	h.GetFeeHistory(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp service.FeeHistory
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Records, 1)
	assert.Len(t, resp.Trends, 1)
}

func TestGetFeeHistory_InvalidFrom(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(&mockFeeService{}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/fees/history?from=bad-date", nil)
	c.Request.URL.RawQuery = "from=bad-date"

	h.GetFeeHistory(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetFeeHistory_ToBeforeFrom(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(&mockFeeService{}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	from := time.Now().UTC().Format(time.RFC3339)
	to := time.Now().UTC().AddDate(0, -1, 0).Format(time.RFC3339)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/fees/history", nil)
	q := c.Request.URL.Query()
	q.Set("from", from)
	q.Set("to", to)
	c.Request.URL.RawQuery = q.Encode()

	h.GetFeeHistory(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetFeeHistory_ServiceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(&mockFeeService{err: errors.New("db error")}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/fees/history", nil)

	h.GetFeeHistory(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
