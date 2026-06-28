package handlers

import (
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

func TestGetFeeHistory_OK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(&mockFeeService{
		history: &service.FeeHistory{
			Records: []service.FeeRecord{{ID: "fee-1", Type: "transaction", Amount: 1.5, Currency: "USD", CreatedAt: time.Now()}},
			Trends:  []service.FeeTrend{{Type: "transaction", Count: 1, TotalAmount: 1.5}},
		},
	})

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
	h := NewFeesHandler(&mockFeeService{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/fees/history?from=bad-date", nil)
	c.Request.URL.RawQuery = "from=bad-date"

	h.GetFeeHistory(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetFeeHistory_ToBeforeFrom(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(&mockFeeService{})

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
	h := NewFeesHandler(&mockFeeService{err: errors.New("db error")})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodGet, "/api/v1/fees/history", nil)

	h.GetFeeHistory(c)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
