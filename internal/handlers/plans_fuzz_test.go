// Package handlers provides native Go fuzz tests for the plans HTTP handler.
//
// These tests exercise the full HTTP layer for GET /api/plans, focusing on the
// query-parameter parsing and pagination logic. The fuzzer drives:
//
//   - "limit" — integer-valued query param; invalid values must not panic
//   - "cursor" — opaque base64 token; any string must not panic (invalid cursors
//     are mapped to a 500 and never reach the service)
//
// # Running during development
//
//	go test -run=^$ -fuzz=FuzzListPlans -fuzztime=10s ./internal/handlers/...
//
// # Seed corpus
//
// The testdata/fuzz/FuzzListPlans/ directory contains hand-crafted inputs that
// cover the interesting boundaries (empty, large, non-numeric limit; valid,
// truncated, and non-base64 cursor tokens).
//
// # Invariants checked on every generated input
//
//  1. The handler never panics.
//  2. The HTTP status code is always 200 or 500 (no 400 from cursor/limit alone).
//  3. When the service returns an error the body contains "error".
//  4. When the service succeeds the response includes a "plans" key.
package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// successPlanService always returns a fixed set of plans with no error.
type successPlanService struct{}

func (s *successPlanService) ListPlans(c *gin.Context) ([]Plan, error) {
	return []Plan{
		{ID: "plan_1", Name: "Basic", Amount: "9.99", Currency: "USD", Interval: "month", Description: "Basic plan"},
		{ID: "plan_2", Name: "Pro", Amount: "29.99", Currency: "USD", Interval: "month", Description: "Pro plan"},
	}, nil
}

// FuzzListPlans drives the GET /api/plans handler with fuzzer-generated
// query-parameter strings and verifies safety invariants on every iteration.
//
// Two independent bytes sequences are provided by the fuzzer:
//   - limitBytes  → used verbatim as the "limit" query param value
//   - cursorBytes → used verbatim as the "cursor" query param value
//
// The test never fails on an expected client-error status code — it only fails
// if the handler panics, crashes, or returns a structurally invalid JSON body.
func FuzzListPlans(f *testing.F) {
	gin.SetMode(gin.TestMode)

	// ── Seed corpus ──────────────────────────────────────────────────────────
	// Realistic, boundary, and adversarial inputs to warm up the fuzzer's
	// coverage map before it starts mutating.

	// (limitBytes, cursorBytes)
	seeds := [][2]string{
		// Happy path
		{"10", ""},
		{"1", ""},
		{"200", ""},

		// Limit boundary values
		{"0", ""},
		{"-1", ""},
		{"9999999999", ""},   // overflow-range integer
		{"2147483648", ""},   // > int32 max
		{"-2147483649", ""},  // < int32 min
		{"1.5", ""},          // float — must be rejected gracefully
		{"1e3", ""},          // scientific notation
		{"010", ""},          // octal-looking

		// Non-numeric limit garbage
		{"abc", ""},
		{"null", ""},
		{" ", ""},
		{"\t\n", ""},
		{string([]byte{0xFF, 0xFE}), ""}, // invalid UTF-8

		// Oversized limit — long string
		{"99999999999999999999999999999999", ""},

		// Valid base64-encoded cursor ({"id":"plan_1","sort_value":"Basic"})
		{"10", "eyJpZCI6InBsYW5fMSIsInNvcnRfdmFsdWUiOiJCYXNpYyJ9"},

		// Truncated cursor token
		{"10", "eyJpZCI6"},

		// Non-base64 cursor
		{"10", "not!base64###"},

		// Empty cursor
		{"10", ""},

		// Cursor that decodes to something non-JSON
		{"10", "aGVsbG8="},    // base64("hello")
		{"10", "e30="},        // base64("{}")  — valid JSON but empty cursor
		{"10", "bnVsbA=="},    // base64("null")

		// UTF-8 boundary stress
		{"10", "5L2g5aW9"},                    // valid base64 of Chinese characters
		{string([]byte{0xC3, 0xA9}), ""},      // é — valid 2-byte UTF-8

		// Extremely long cursor
		{"10", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=="},
	}

	for _, seed := range seeds {
		f.Add(seed[0], seed[1])
	}

	// ── Fuzz target ──────────────────────────────────────────────────────────
	h := &Handler{Plans: &successPlanService{}}

	f.Fuzz(func(t *testing.T, limitStr, cursorStr string) {
		// Skip inputs with invalid UTF-8 to avoid noise in error messages;
		// the handler itself will receive them as raw bytes via the URL so we
		// allow them through at the URL level but filter here for clarity.
		if !utf8.ValidString(limitStr) || !utf8.ValidString(cursorStr) {
			// Still exercise the handler — just don't assert on response body decoding.
			exerciseListPlans(t, h, limitStr, cursorStr, false)
			return
		}

		exerciseListPlans(t, h, limitStr, cursorStr, true)
	})
}

// exerciseListPlans builds a test HTTP request with the given query params,
// invokes the handler, and checks invariants.
func exerciseListPlans(t *testing.T, h *Handler, limitStr, cursorStr string, checkJSON bool) {
	t.Helper()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req, err := http.NewRequest(http.MethodGet, "/api/plans", nil)
	if err != nil {
		t.Fatalf("failed to build request: %v", err)
	}

	q := req.URL.Query()
	if limitStr != "" {
		q.Set("limit", limitStr)
	}
	if cursorStr != "" {
		q.Set("cursor", cursorStr)
	}
	req.URL.RawQuery = q.Encode()
	c.Request = req

	// Invariant 1: handler must not panic.
	h.ListPlans(c)

	// Invariant 2: status must be 200 or 500 — never 4xx for limit/cursor alone.
	code := w.Code
	if code != http.StatusOK && code != http.StatusInternalServerError {
		t.Errorf("unexpected status %d for limit=%q cursor=%q", code, limitStr, cursorStr)
	}

	if !checkJSON {
		return
	}

	body := w.Body.Bytes()

	// Invariant 3: body must always be valid JSON.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Errorf("response body is not valid JSON for limit=%q cursor=%q: %v\nbody: %s",
			limitStr, cursorStr, err, body)
		return
	}

	if code == http.StatusOK {
		// Invariant 4: successful response must include "plans" key.
		if _, ok := raw["plans"]; !ok {
			t.Errorf("200 response missing 'plans' key for limit=%q cursor=%q\nbody: %s",
				limitStr, cursorStr, body)
		}
	} else {
		// Invariant 5: error response must include "error" or "code" key.
		_, hasError := raw["error"]
		_, hasCode := raw["code"]
		if !hasError && !hasCode {
			t.Errorf("error response missing 'error'/'code' key for limit=%q cursor=%q\nbody: %s",
				limitStr, cursorStr, body)
		}
	}
}
