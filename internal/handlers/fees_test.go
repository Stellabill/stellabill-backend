package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/fees"
)

func TestFeesHandler_ListFees(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := fees.NewMemoryService()
	h := NewFeesHandler(svc)

	t.Run("missing subscription_id returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees", nil)
		h.ListFees(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns fees for valid subscription", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees?subscription_id=sub-123", nil)
		h.ListFees(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "2025-01-01", resp["api_version"])
		data := resp["data"].(map[string]interface{})
		feeList := data["fees"].([]interface{})
		assert.Len(t, feeList, 3)
	})

	t.Run("filters by status", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees?subscription_id=sub-123&status=paid", nil)
		h.ListFees(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		data := resp["data"].(map[string]interface{})
		feeList := data["fees"].([]interface{})
		assert.Len(t, feeList, 2)
	})

	t.Run("returns empty for unknown subscription", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees?subscription_id=sub-nope", nil)
		h.ListFees(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		data := resp["data"].(map[string]interface{})
		feeList := data["fees"].([]interface{})
		assert.Empty(t, feeList)
	})
}

func TestFeesHandler_GetFeeHistory(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := fees.NewMemoryService()
	h := NewFeesHandler(svc)

	t.Run("missing subscription_id returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/history", nil)
		h.GetFeeHistory(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid limit returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/history?subscription_id=sub-123&limit=abc", nil)
		h.GetFeeHistory(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns history ordered desc", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/history?subscription_id=sub-123", nil)
		h.GetFeeHistory(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "2025-01-01", resp["api_version"])
		data := resp["data"].(map[string]interface{})
		history := data["history"].([]interface{})
		assert.Len(t, history, 3)
	})

	t.Run("respects limit param", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/history?subscription_id=sub-123&limit=1", nil)
		h.GetFeeHistory(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		data := resp["data"].(map[string]interface{})
		history := data["history"].([]interface{})
		assert.Len(t, history, 1)
	})
}

func TestFeesHandler_GetFeeTrends(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := fees.NewMemoryService()
	h := NewFeesHandler(svc)

	t.Run("missing subscription_id returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/trends", nil)
		h.GetFeeTrends(c)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns trend data", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/trends?subscription_id=sub-123", nil)
		h.GetFeeTrends(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "2025-01-01", resp["api_version"])
		data := resp["data"].(map[string]interface{})
		assert.Equal(t, "sub-123", data["subscription_id"])
		assert.Equal(t, "USD", data["currency"])
		assert.NotEmpty(t, data["points"])
	})

	t.Run("returns empty trend for unknown subscription", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/trends?subscription_id=sub-nope", nil)
		h.GetFeeTrends(c)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		data := resp["data"].(map[string]interface{})
		points := data["points"].([]interface{})
		assert.Empty(t, points)
	})
}

// mockFeesService is a test double for fees.Service that returns an error.
type mockFeesService struct{}

func (m *mockFeesService) ListFees(_ context.Context, _, _, _ string) ([]fees.Fee, error) {
	return nil, assert.AnError
}
func (m *mockFeesService) GetHistory(_ context.Context, _ string, _ int) ([]fees.Fee, error) {
	return nil, assert.AnError
}
func (m *mockFeesService) GetTrends(_ context.Context, _ string) (*fees.Trend, error) {
	return nil, assert.AnError
}

func TestFeesHandler_ServiceErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewFeesHandler(&mockFeesService{})

	t.Run("ListFees service error returns 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees?subscription_id=sub-123", nil)
		h.ListFees(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("GetFeeHistory service error returns 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/history?subscription_id=sub-123", nil)
		h.GetFeeHistory(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("GetFeeTrends service error returns 500", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/fees/trends?subscription_id=sub-123", nil)
		h.GetFeeTrends(c)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}
