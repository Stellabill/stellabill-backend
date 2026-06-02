package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"stellarbill-backend/internal/service"
)

// mockSwapRouter implements service.SwapRouter for tests.
type mockSwapRouter struct {
	result *service.SwapResult
	err    error
}

func (m *mockSwapRouter) SwapExactTokensForTokens(_, _ string, amountIn, _ float64) (*service.SwapResult, error) {
	return m.result, m.err
}
func (m *mockSwapRouter) SwapTokensForExactTokens(_, _ string, amountOut, _ float64) (*service.SwapResult, error) {
	return m.result, m.err
}

func postJSON(t *testing.T, h *SwapHandler, path string, body interface{}, fn func(*gin.Context)) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	c.Request.Header.Set("Content-Type", "application/json")
	fn(c)
	return w
}

func TestSwapExactIn_OK(t *testing.T) {
	h := NewSwapHandler(&mockSwapRouter{result: &service.SwapResult{TokenIn: "USDC", TokenOut: "XLM", AmountIn: 100, AmountOut: 99.7, Fee: 0.3}})
	w := postJSON(t, h, "/api/v1/swap/exact-in", map[string]interface{}{
		"token_in": "USDC", "token_out": "XLM", "amount_in": 100.0, "min_amount_out": 0.0,
	}, h.SwapExactTokensForTokens)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp service.SwapResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "USDC", resp.TokenIn)
}

func TestSwapExactIn_BadRequest(t *testing.T) {
	h := NewSwapHandler(&mockSwapRouter{})
	w := postJSON(t, h, "/api/v1/swap/exact-in", map[string]interface{}{
		"token_in": "USDC",
	}, h.SwapExactTokensForTokens)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSwapExactIn_InsufficientLiquidity(t *testing.T) {
	h := NewSwapHandler(&mockSwapRouter{err: service.ErrInsufficientLiquidity})
	w := postJSON(t, h, "/api/v1/swap/exact-in", map[string]interface{}{
		"token_in": "USDC", "token_out": "XLM", "amount_in": 100.0, "min_amount_out": 0.0,
	}, h.SwapExactTokensForTokens)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestSwapExactIn_ServiceError(t *testing.T) {
	h := NewSwapHandler(&mockSwapRouter{err: errors.New("unexpected")})
	w := postJSON(t, h, "/api/v1/swap/exact-in", map[string]interface{}{
		"token_in": "USDC", "token_out": "XLM", "amount_in": 100.0, "min_amount_out": 0.0,
	}, h.SwapExactTokensForTokens)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSwapExactOut_OK(t *testing.T) {
	h := NewSwapHandler(&mockSwapRouter{result: &service.SwapResult{TokenIn: "USDC", TokenOut: "XLM", AmountIn: 100.3, AmountOut: 100, Fee: 0.3}})
	w := postJSON(t, h, "/api/v1/swap/exact-out", map[string]interface{}{
		"token_in": "USDC", "token_out": "XLM", "amount_out": 100.0, "max_amount_in": 200.0,
	}, h.SwapTokensForExactTokens)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp service.SwapResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 100.0, resp.AmountOut)
}

func TestSwapExactOut_BadRequest(t *testing.T) {
	h := NewSwapHandler(&mockSwapRouter{})
	w := postJSON(t, h, "/api/v1/swap/exact-out", map[string]interface{}{
		"token_out": "XLM",
	}, h.SwapTokensForExactTokens)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSwapExactOut_InsufficientLiquidity(t *testing.T) {
	h := NewSwapHandler(&mockSwapRouter{err: service.ErrInsufficientLiquidity})
	w := postJSON(t, h, "/api/v1/swap/exact-out", map[string]interface{}{
		"token_in": "USDC", "token_out": "XLM", "amount_out": 100.0, "max_amount_in": 200.0,
	}, h.SwapTokensForExactTokens)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}
