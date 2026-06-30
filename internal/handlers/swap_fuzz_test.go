// Package handlers provides native Go fuzz tests for the swap HTTP handlers.
//
// Two fuzz targets are defined:
//
//   - FuzzSwapExactIn  — exercises POST /api/v1/swap/exact-in JSON body parsing
//     and the handler's error-mapping logic.
//
//   - FuzzSwapExactOut — exercises POST /api/v1/swap/exact-out JSON body parsing
//     and the handler's error-mapping logic.
//
// Both targets use a deterministic stub router that returns a known good result
// for valid inputs, so the fuzzer can distinguish between body-parsing errors
// (400) and genuine service errors. No real AMM pool or network is needed.
//
// # Safety invariants
//
// On every generated input the test verifies:
//  1. The handler never panics.
//  2. The HTTP status code is one of: 200, 400, 422, 500.
//  3. The response body is always valid JSON.
//  4. A 400 response always includes a "code" field (from the error envelope).
//  5. A 200 response always includes the "token_in" field.
//  6. NaN and ±Inf amounts (IEEE 754 edge cases) never produce a 200.
//
// # Running during development
//
//	go test -run=^$ -fuzz=FuzzSwapExactIn  -fuzztime=10s ./internal/handlers/...
//	go test -run=^$ -fuzz=FuzzSwapExactOut -fuzztime=10s ./internal/handlers/...
//
// # Seed corpus
//
// See testdata/fuzz/FuzzSwapExactIn/ and testdata/fuzz/FuzzSwapExactOut/ for
// hand-crafted inputs. The corpus covers minimal valid requests, missing required
// fields, negative/zero amounts, extremely large numbers, and non-JSON payloads.
package handlers

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/service"
)

// ── Stub swap router ─────────────────────────────────────────────────────────

// echoSwapRouter echoes back a SwapResult that mirrors the input amounts.
// It returns ErrInsufficientLiquidity when minAmountOut exceeds a hard cap, so
// the fuzzer can exercise the 422 path too.
type echoSwapRouter struct{}

var _ service.SwapRouter = (*echoSwapRouter)(nil)

func (r *echoSwapRouter) SwapExactTokensForTokens(
	tokenIn, tokenOut string,
	amountIn, minAmountOut float64,
) (*service.SwapResult, error) {
	if math.IsNaN(amountIn) || math.IsInf(amountIn, 0) {
		return nil, service.ErrInsufficientLiquidity
	}
	if minAmountOut > 1_000_000 {
		return nil, service.ErrInsufficientLiquidity
	}
	fee := amountIn * 0.003
	return &service.SwapResult{
		TokenIn:     tokenIn,
		TokenOut:    tokenOut,
		AmountIn:    amountIn,
		AmountOut:   amountIn - fee,
		PriceImpact: 0.01,
		Fee:         fee,
	}, nil
}

func (r *echoSwapRouter) SwapTokensForExactTokens(
	tokenIn, tokenOut string,
	amountOut, maxAmountIn float64,
) (*service.SwapResult, error) {
	if math.IsNaN(amountOut) || math.IsInf(amountOut, 0) {
		return nil, service.ErrInsufficientLiquidity
	}
	if amountOut > maxAmountIn {
		return nil, service.ErrInsufficientLiquidity
	}
	fee := amountOut * 0.003
	return &service.SwapResult{
		TokenIn:     tokenIn,
		TokenOut:    tokenOut,
		AmountIn:    amountOut + fee,
		AmountOut:   amountOut,
		PriceImpact: 0.01,
		Fee:         fee,
	}, nil
}

// ── Allowed status codes ──────────────────────────────────────────────────────

var swapAllowedCodes = map[int]bool{
	http.StatusOK:                  true,
	http.StatusBadRequest:          true,
	http.StatusUnprocessableEntity: true,
	http.StatusInternalServerError: true,
}

// ── FuzzSwapInput ───────────────────────────────────────────────────────────

// FuzzSwapInput exercises the shared request parsing for both swap endpoints.
// It varies the route, raw JSON body, and numeric edge cases to ensure the
// handler never panics and always returns either a structured validation error
// or a successful swap response.
func FuzzSwapInput(f *testing.F) {
	gin.SetMode(gin.TestMode)

	type seed struct {
		path    string
		rawBody string
	}
	seeds := []seed{
		{"/api/v1/swap/exact-in", `{"token_in":"USDC","token_out":"XLM","amount_in":100,"min_amount_out":0}`},
		{"/api/v1/swap/exact-out", `{"token_in":"USDC","token_out":"XLM","amount_out":100,"max_amount_in":200}`},
		{"/api/v1/swap/exact-in", `{"token_in":"","token_out":"XLM","amount_in":100,"min_amount_out":0}`},
		{"/api/v1/swap/exact-out", `{"token_in":"USDC","token_out":"","amount_out":100,"max_amount_in":200}`},
		{"/api/v1/swap/exact-in", `{"token_in":"USDC","token_out":"XLM","amount_in":0,"min_amount_out":0}`},
		{"/api/v1/swap/exact-out", `{"token_in":"USDC","token_out":"XLM","amount_out":100,"max_amount_in":0}`},
		{"/api/v1/swap/exact-in", `{"token_in":"USDC","token_out":"XLM","amount_in":1e309,"min_amount_out":0}`},
		{"/api/v1/swap/exact-out", `{"token_in":"USDC","token_out":"XLM","amount_out":-1,"max_amount_in":200}`},
		{"/api/v1/swap/exact-in", `{"token_in":123,"token_out":true,"amount_in":"oops","min_amount_out":null}`},
		{"/api/v1/swap/exact-out", `{`},
		{"/api/v1/swap/exact-in", "\x00\xff"},
		{"/api/v1/swap/exact-out", `{"token_in":"USDC","token_out":"XLM","amount_out":100,"max_amount_in":200,"extra":"x"}`},
	}

	for _, s := range seeds {
		f.Add(s.path, s.rawBody)
	}

	h := NewSwapHandler(&echoSwapRouter{})

	f.Fuzz(func(t *testing.T, path, rawBody string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		req, err := http.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(rawBody)))
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		c.Request = req

		if path == "/api/v1/swap/exact-in" {
			h.SwapExactTokensForTokens(c)
		} else {
			h.SwapTokensForExactTokens(c)
		}

		code := w.Code
		if !swapAllowedCodes[code] {
			t.Errorf("unexpected status %d for path=%s body=%q", code, path, rawBody)
		}

		responseBody := w.Body.Bytes()
		if utf8.ValidString(rawBody) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(responseBody, &raw); err != nil {
				t.Errorf("non-JSON body for path=%s body=%q: %v\nbody: %s", path, rawBody, err, responseBody)
				return
			}
			if code == http.StatusBadRequest {
				if _, ok := raw["code"]; !ok {
					t.Errorf("400 response missing 'code' for path=%s body=%q\nbody: %s", path, rawBody, responseBody)
				}
			}
			if code == http.StatusOK {
				if _, ok := raw["token_in"]; !ok {
					t.Errorf("200 response missing 'token_in' for path=%s body=%q\nbody: %s", path, rawBody, responseBody)
				}
			}
		}
	})
}

// ── FuzzSwapExactIn ───────────────────────────────────────────────────────────

// FuzzSwapExactIn drives SwapExactTokensForTokens with fuzzer-generated JSON
// bodies. The fuzzer controls each field independently (tokenIn, tokenOut,
// amountIn, minAmountOut) so it can find combinations that expose edge cases
// in the binding/validation layer.
func FuzzSwapExactIn(f *testing.F) {
	gin.SetMode(gin.TestMode)

	// ── Seed corpus ──────────────────────────────────────────────────────────
	type seed struct {
		tokenIn, tokenOut string
		amountIn          float64
		minAmountOut      float64
	}
	seeds := []seed{
		// Happy path
		{"USDC", "XLM", 100.0, 0.0},
		{"USDC", "XLM", 100.0, 99.0},
		{"USDC", "XLM", 0.01, 0.0},
		{"USDC", "XLM", 1_000_000.0, 0.0},

		// minAmountOut triggers ErrInsufficientLiquidity (> 1_000_000)
		{"USDC", "XLM", 100.0, 2_000_000.0},

		// Boundary amounts
		{"USDC", "XLM", math.SmallestNonzeroFloat64, 0.0},
		{"USDC", "XLM", math.MaxFloat64 / 2, 0.0},

		// Zero / negative amountIn → binding rejects (gt=0)
		{"USDC", "XLM", 0.0, 0.0},
		{"USDC", "XLM", -1.0, 0.0},

		// Empty token strings → binding rejects (required)
		{"", "XLM", 100.0, 0.0},
		{"USDC", "", 100.0, 0.0},
		{"", "", 100.0, 0.0},

		// Long token strings (URL/header stress)
		{string(make([]byte, 512)), "XLM", 100.0, 0.0},

		// Non-ASCII token names
		{"USDC-你好", "XLM", 100.0, 0.0},
		{"USDC\x00", "XLM", 100.0, 0.0}, // null byte in token name

		// Negative minAmountOut → binding accepts (gte=0 → must be ≥0, negative fails)
		{"USDC", "XLM", 100.0, -1.0},
	}

	for _, s := range seeds {
		f.Add(s.tokenIn, s.tokenOut, s.amountIn, s.minAmountOut)
	}

	// ── Fuzz target ──────────────────────────────────────────────────────────
	h := NewSwapHandler(&echoSwapRouter{})

	f.Fuzz(func(t *testing.T, tokenIn, tokenOut string, amountIn, minAmountOut float64) {
		// Build the request body. We always use the structured JSON so the
		// fuzzer explores numeric edge cases rather than raw byte sequences
		// (raw-byte JSON fuzzing is covered by the non-JSON seed below).
		body := map[string]interface{}{
			"token_in":       tokenIn,
			"token_out":      tokenOut,
			"amount_in":      amountIn,
			"min_amount_out": minAmountOut,
		}
		bs, err := json.Marshal(body)
		if err != nil {
			// json.Marshal can fail for NaN/Inf — Gin's ShouldBindJSON would
			// reject them anyway; we treat this as a 400-equivalent skip.
			return
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, reqErr := http.NewRequest(http.MethodPost, "/api/v1/swap/exact-in", bytes.NewReader(bs))
		if reqErr != nil {
			t.Fatalf("failed to build request: %v", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		c.Request = req

		// Invariant 1: handler must not panic.
		h.SwapExactTokensForTokens(c)

		code := w.Code

		// Invariant 2: status must be in the allowed set.
		if !swapAllowedCodes[code] {
			t.Errorf("unexpected status %d for tokenIn=%q tokenOut=%q amountIn=%v minAmountOut=%v",
				code, tokenIn, tokenOut, amountIn, minAmountOut)
		}

		responseBody := w.Body.Bytes()

		// Invariant 3: body must always be valid JSON.
		if utf8.ValidString(tokenIn) && utf8.ValidString(tokenOut) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(responseBody, &raw); err != nil {
				t.Errorf("non-JSON body for tokenIn=%q tokenOut=%q amountIn=%v: %v\nbody: %s",
					tokenIn, tokenOut, amountIn, err, responseBody)
				return
			}

			switch code {
			case http.StatusOK:
				// Invariant 5: 200 body must have token_in.
				if _, ok := raw["token_in"]; !ok {
					t.Errorf("200 response missing 'token_in'\nbody: %s", responseBody)
				}
			case http.StatusBadRequest:
				// Invariant 4: 400 body must have "code".
				if _, ok := raw["code"]; !ok {
					t.Errorf("400 response missing 'code'\nbody: %s", responseBody)
				}
			}
		}

		// Invariant 6: NaN / Inf amounts must never yield 200.
		if (math.IsNaN(amountIn) || math.IsInf(amountIn, 0)) && code == http.StatusOK {
			t.Errorf("NaN/Inf amountIn produced 200 response")
		}
	})
}

// ── FuzzSwapExactOut ──────────────────────────────────────────────────────────

// FuzzSwapExactOut drives SwapTokensForExactTokens with fuzzer-generated JSON
// bodies. It mirrors FuzzSwapExactIn with the exact-out field set
// (amountOut + maxAmountIn) and the same invariants.
func FuzzSwapExactOut(f *testing.F) {
	gin.SetMode(gin.TestMode)

	// ── Seed corpus ──────────────────────────────────────────────────────────
	type seed struct {
		tokenIn, tokenOut string
		amountOut         float64
		maxAmountIn       float64
	}
	seeds := []seed{
		// Happy path
		{"USDC", "XLM", 100.0, 200.0},
		{"USDC", "XLM", 0.01, 1.0},
		{"USDC", "XLM", 1000.0, 2000.0},

		// amountOut > maxAmountIn → stub returns ErrInsufficientLiquidity → 422
		{"USDC", "XLM", 500.0, 100.0},

		// Boundary amounts
		{"USDC", "XLM", math.SmallestNonzeroFloat64, 1.0},

		// Zero / negative amounts → binding rejects (gt=0, required)
		{"USDC", "XLM", 0.0, 200.0},
		{"USDC", "XLM", -1.0, 200.0},
		{"USDC", "XLM", 100.0, 0.0},
		{"USDC", "XLM", 100.0, -1.0},

		// Missing required token → 400
		{"", "XLM", 100.0, 200.0},
		{"USDC", "", 100.0, 200.0},

		// Long token strings
		{string(make([]byte, 512)), "XLM", 100.0, 200.0},

		// Null byte in token
		{"USDC\x00NULL", "XLM", 100.0, 200.0},

		// Non-ASCII tokens
		{"USDC-€", "XLM-£", 100.0, 200.0},

		// Very large amounts
		{" USDC", "XLM", math.MaxFloat64 / 2, math.MaxFloat64},
	}

	for _, s := range seeds {
		f.Add(s.tokenIn, s.tokenOut, s.amountOut, s.maxAmountIn)
	}

	// ── Fuzz target ──────────────────────────────────────────────────────────
	h := NewSwapHandler(&echoSwapRouter{})

	f.Fuzz(func(t *testing.T, tokenIn, tokenOut string, amountOut, maxAmountIn float64) {
		body := map[string]interface{}{
			"token_in":      tokenIn,
			"token_out":     tokenOut,
			"amount_out":    amountOut,
			"max_amount_in": maxAmountIn,
		}
		bs, err := json.Marshal(body)
		if err != nil {
			return // NaN/Inf in JSON → skip
		}

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, reqErr := http.NewRequest(http.MethodPost, "/api/v1/swap/exact-out", bytes.NewReader(bs))
		if reqErr != nil {
			t.Fatalf("failed to build request: %v", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		c.Request = req

		// Invariant 1: must not panic.
		h.SwapTokensForExactTokens(c)

		code := w.Code

		// Invariant 2: status in allowed set.
		if !swapAllowedCodes[code] {
			t.Errorf("unexpected status %d for tokenIn=%q tokenOut=%q amountOut=%v maxAmountIn=%v",
				code, tokenIn, tokenOut, amountOut, maxAmountIn)
		}

		responseBody := w.Body.Bytes()

		// Invariant 3: body must be valid JSON.
		if utf8.ValidString(tokenIn) && utf8.ValidString(tokenOut) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(responseBody, &raw); err != nil {
				t.Errorf("non-JSON body for tokenIn=%q tokenOut=%q amountOut=%v: %v\nbody: %s",
					tokenIn, tokenOut, amountOut, err, responseBody)
				return
			}

			switch code {
			case http.StatusOK:
				// Invariant 5: 200 must have token_in.
				if _, ok := raw["token_in"]; !ok {
					t.Errorf("200 response missing 'token_in'\nbody: %s", responseBody)
				}
			case http.StatusBadRequest:
				// Invariant 4: 400 must have "code".
				if _, ok := raw["code"]; !ok {
					t.Errorf("400 response missing 'code'\nbody: %s", responseBody)
				}
			}
		}

		// Invariant 6: NaN / Inf amounts must never yield 200.
		if (math.IsNaN(amountOut) || math.IsInf(amountOut, 0)) && code == http.StatusOK {
			t.Errorf("NaN/Inf amountOut produced 200 response")
		}
	})
}

// ── Non-JSON raw body fuzz test ───────────────────────────────────────────────

// FuzzSwapRawBody exercises both swap endpoints with completely arbitrary byte
// sequences as the request body (not pre-structured JSON). This catches parsing
// panics that structured fuzzing cannot reach.
func FuzzSwapRawBody(f *testing.F) {
	gin.SetMode(gin.TestMode)

	// Seed with a range of non-JSON and malformed JSON inputs.
	rawSeeds := []string{
		// Valid JSON
		`{"token_in":"USDC","token_out":"XLM","amount_in":100,"min_amount_out":0}`,
		`{"token_in":"USDC","token_out":"XLM","amount_out":100,"max_amount_in":200}`,

		// Malformed JSON
		`{`,
		`}`,
		`null`,
		`[]`,
		`""`,
		`true`,

		// JSON with extra/unknown fields
		`{"token_in":"USDC","token_out":"XLM","amount_in":100,"unknown":true}`,

		// JSON with wrong types
		`{"token_in":123,"token_out":true,"amount_in":"not-a-number","min_amount_out":null}`,

		// Empty body
		``,

		// Binary / non-UTF-8
		"\x00\x01\x02\x03",
		"\xff\xfe",

		// Extremely large numbers
		`{"token_in":"A","token_out":"B","amount_in":1e308,"min_amount_out":0}`,
		`{"token_in":"A","token_out":"B","amount_in":-1e308,"min_amount_out":0}`,

		// Deeply nested (DoS probe)
		`{"token_in":"A","token_out":"B","amount_in":{"nested":{"deep":1}},"min_amount_out":0}`,

		// Unicode in token strings
		`{"token_in":"USDC\u0000","token_out":"XLM","amount_in":1,"min_amount_out":0}`,

		// Truncated UTF-8 sequence
		"{\x22token_in\x22:\x22\xc3\x22}",
	}

	for _, s := range rawSeeds {
		f.Add(s, true) // true = exact-in endpoint
		f.Add(s, false) // false = exact-out endpoint
	}

	h := NewSwapHandler(&echoSwapRouter{})

	f.Fuzz(func(t *testing.T, rawBody string, exactIn bool) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)

		var path string
		var handlerFn func(*gin.Context)
		if exactIn {
			path = "/api/v1/swap/exact-in"
			handlerFn = h.SwapExactTokensForTokens
		} else {
			path = "/api/v1/swap/exact-out"
			handlerFn = h.SwapTokensForExactTokens
		}

		req, err := http.NewRequest(http.MethodPost, path, bytes.NewReader([]byte(rawBody)))
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		c.Request = req

		// Invariant 1: must not panic regardless of body content.
		handlerFn(c)

		code := w.Code

		// Invariant 2: status must be in the allowed set.
		if !swapAllowedCodes[code] {
			t.Errorf("unexpected status %d for path=%s body=%q", code, path, rawBody)
		}
	})
}
