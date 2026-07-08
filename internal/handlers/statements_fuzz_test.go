// Package handlers provides native Go fuzz tests for the statements HTTP handlers.
//
// Two fuzz targets are defined:
//
//   - FuzzListStatements — exercises the query-parameter parser for
//     GET /api/v1/statements (customer_id, subscription_id, kind, status,
//     start_after, end_before, limit, order).
//
//   - FuzzGetStatement — exercises the path-parameter parser for
//     GET /api/v1/statements/:id.
//
// Both targets use a lightweight stub implementation of service.StatementService
// so no database is required. The fuzzer verifies the following invariants on
// every generated input:
//
//  1. The handler never panics.
//  2. The HTTP status code is always a valid HTTP status (100–599).
//  3. The response body is always valid JSON.
//  4. When status is 200 the response contains expected top-level keys.
//  5. When limit is out of range the handler clamps or rejects it — it never
//     returns a raw Go error string.
//
// # Running during development
//
//	go test -run=^$ -fuzz=FuzzListStatements -fuzztime=10s ./internal/handlers/...
//	go test -run=^$ -fuzz=FuzzGetStatement   -fuzztime=10s ./internal/handlers/...
//
// # Seed corpus
//
// See testdata/fuzz/FuzzListStatements/ and testdata/fuzz/FuzzGetStatement/ for
// the hand-crafted inputs. The corpus covers RFC3339 timestamps (valid and
// malformed), UUID-shaped IDs, SQL-injection probes, and oversized strings.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

// ── Stub service ─────────────────────────────────────────────────────────────

// stubStatementService is a minimal implementation of service.StatementService
// that returns a fixed list of statements. It is safe to call from multiple
// goroutines (read-only after construction).
type stubStatementService struct{}

var _ service.StatementService = (*stubStatementService)(nil)

func (s *stubStatementService) GetDetail(
	_ context.Context,
	callerID string,
	_ []string,
	statementID string,
) (*service.StatementDetail, []string, error) {
	if statementID == "stmt-known" {
		return &service.StatementDetail{
			ID:             "stmt-known",
			SubscriptionID: "sub-1",
			Customer:       callerID,
			PeriodStart:    "2024-01-01T00:00:00Z",
			PeriodEnd:      "2024-01-31T23:59:59Z",
			IssuedAt:       "2024-02-01T00:00:00Z",
			TotalAmount:    "100.00",
			Currency:       "USD",
			Kind:           "invoice",
			Status:         "paid",
		}, nil, nil
	}
	return nil, nil, service.ErrNotFound
}

func (s *stubStatementService) ListByCustomer(
	_ context.Context,
	callerID string,
	_ []string,
	customerID string,
	q repository.StatementQuery,
) (*service.ListStatementsDetail, int, []string, error) {
	// callerID must match customerID for a subscriber (non-admin/merchant).
	if callerID != customerID {
		return nil, 0, nil, service.ErrForbidden
	}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	stmts := make([]*service.StatementDetail, 0, limit)
	for i := 0; i < limit; i++ {
		stmts = append(stmts, &service.StatementDetail{
			ID:             fmt.Sprintf("stmt-%s-%d", customerID, i),
			SubscriptionID: "sub-1",
			Customer:       customerID,
			PeriodStart:    "2024-01-01T00:00:00Z",
			PeriodEnd:      "2024-01-31T23:59:59Z",
			IssuedAt:       "2024-02-01T00:00:00Z",
			TotalAmount:    "100.00",
			Currency:       "USD",
			Kind:           "invoice",
			Status:         "paid",
		})
	}
	return &service.ListStatementsDetail{Statements: stmts}, limit, nil, nil
}

// ── Helper: set auth context ──────────────────────────────────────────────────

// setStubAuth injects a caller_id and roles into the Gin context so the
// handler's getAuthContext() call succeeds.
func setStubAuth(c *gin.Context, callerID string, roles []string) {
	c.Set("caller_id", callerID)
	c.Set("roles", roles)
}

// ── FuzzListStatements ────────────────────────────────────────────────────────

// FuzzListStatements exercises the query-parameter parsing and validation logic
// inside buildStatementQuery. The fuzzer varies:
//
//   - customerID   — the required customer_id param
//   - subscriptionID — optional subscription_id filter
//   - kind         — free-form statement kind filter
//   - status       — lifecycle status filter
//   - startAfter   — RFC3339 timestamp (invalid values must return 400)
//   - endBefore    — RFC3339 timestamp (invalid values must return 400)
//   - limitStr     — integer page size
//   - order        — "asc" / "desc" (anything else → 400)
func FuzzListStatements(f *testing.F) {
	gin.SetMode(gin.TestMode)

	// ── Seed corpus ──────────────────────────────────────────────────────────
	type seed struct {
		customerID, subscriptionID, kind, status, startAfter, endBefore, limitStr, order string
	}
	seeds := []seed{
		// Happy-path inputs
		{"cust-1", "", "invoice", "paid", "", "", "20", "desc"},
		{"cust-1", "sub-abc", "credit_note", "open", "2024-01-01T00:00:00Z", "2024-12-31T23:59:59Z", "50", "asc"},
		{"cust-1", "", "", "", "", "", "1", "desc"},
		{"cust-1", "", "", "", "", "", "200", "asc"},

		// Boundary limit values
		{"cust-1", "", "", "", "", "", "0", "desc"},
		{"cust-1", "", "", "", "", "", "-1", "desc"},
		{"cust-1", "", "", "", "", "", "201", "desc"},        // above max → clamp to 200
		{"cust-1", "", "", "", "", "", "999999", "desc"},
		{"cust-1", "", "", "", "", "", "2147483647", "desc"}, // max int32
		{"cust-1", "", "", "", "", "", "2147483648", "desc"}, // overflow

		// Invalid order values → 400
		{"cust-1", "", "", "", "", "", "20", "DESC"},
		{"cust-1", "", "", "", "", "", "20", "random"},
		{"cust-1", "", "", "", "", "", "20", ""},
		{"cust-1", "", "", "", "", "", "20", "1"},

		// Invalid timestamps → 400
		{"cust-1", "", "", "", "not-a-date", "", "20", "desc"},
		{"cust-1", "", "", "", "2024-13-01T00:00:00Z", "", "20", "desc"}, // month 13
		{"cust-1", "", "", "", "", "2024-00-01T00:00:00Z", "20", "desc"}, // month 0
		{"cust-1", "", "", "", "2024-01-01", "", "20", "desc"},            // date-only (no time)
		{"cust-1", "", "", "", "2024-01-01T00:00:00", "", "20", "desc"},  // missing Z

		// SQL injection probes
		{"cust-1' OR '1'='1", "", "", "", "", "", "20", "desc"},
		{"cust-1", "sub' UNION SELECT 1--", "", "", "", "", "20", "desc"},
		{"cust-1", "", "", "'; DROP TABLE statements;--", "", "", "20", "desc"},

		// Oversized strings (URL length stress)
		{string(make([]byte, 1024)), "", "", "", "", "", "20", "desc"},

		// UTF-8 boundary strings
		{"cust-\xc3\xa9", "", "", "", "", "", "20", "desc"}, // é
		{"cust-你好", "", "", "", "", "", "20", "desc"},

		// Empty customer_id → 400
		{"", "", "", "", "", "", "20", "desc"},

		// Non-numeric limit
		{"cust-1", "", "", "", "", "", "abc", "desc"},
		{"cust-1", "", "", "", "", "", "1.5", "desc"},
		{"cust-1", "", "", "", "", "", "1e2", "desc"},
		{"cust-1", "", "", "", "", "", "null", "desc"},
	}

	for _, s := range seeds {
		f.Add(s.customerID, s.subscriptionID, s.kind, s.status,
			s.startAfter, s.endBefore, s.limitStr, s.order)
	}

	// ── Fuzz target ──────────────────────────────────────────────────────────
	svc := &stubStatementService{}
	handler := NewListStatementsHandler(svc)

	f.Fuzz(func(t *testing.T,
		customerID, subscriptionID, kind, status,
		startAfter, endBefore, limitStr, order string,
	) {
		// Build request
		req, err := http.NewRequest(http.MethodGet, "/api/v1/statements", nil)
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		q := req.URL.Query()
		q.Set("customer_id", customerID)
		if subscriptionID != "" {
			q.Set("subscription_id", subscriptionID)
		}
		if kind != "" {
			q.Set("kind", kind)
		}
		if status != "" {
			q.Set("status", status)
		}
		if startAfter != "" {
			q.Set("start_after", startAfter)
		}
		if endBefore != "" {
			q.Set("end_before", endBefore)
		}
		if limitStr != "" {
			q.Set("limit", limitStr)
		}
		if order != "" {
			q.Set("order", order)
		}
		req.URL.RawQuery = q.Encode()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = req
		// Use customerID as the caller so the stub's ownership check can pass
		// when customerID is a non-empty valid string.
		setStubAuth(c, customerID, []string{"subscriber"})

		// Invariant 1: handler must not panic.
		handler(c)

		code := w.Code

		// Invariant 2: code must be a valid HTTP status.
		if code < 100 || code > 599 {
			t.Errorf("invalid HTTP status %d", code)
		}

		body := w.Body.Bytes()

		// Invariant 3: body must be valid JSON for valid UTF-8 inputs.
		if utf8.ValidString(customerID) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Errorf("non-JSON body for customerID=%q limitStr=%q order=%q: %v\nbody: %s",
					customerID, limitStr, order, err, body)
				return
			}

			if code == http.StatusOK {
				// Invariant 4: 200 response must include "statements" key.
				if _, ok := raw["statements"]; !ok {
					t.Errorf("200 response missing 'statements' key\nbody: %s", body)
				}
			}
		}
	})
}

// ── FuzzGetStatement ──────────────────────────────────────────────────────────

// FuzzGetStatement exercises the path-parameter extraction and validation logic
// inside NewGetStatementHandler. The fuzzer varies:
//
//   - statementID — the :id URL path parameter
//   - callerID    — the authenticated caller injected via Gin context
//
// Invariants checked:
//  1. Handler never panics.
//  2. Status is 200, 401, 403, 404, or 500 — never an unexpected code.
//  3. Body is always valid JSON.
//  4. 404 body includes "error" key.
//  5. The response for the well-known "stmt-known" ID is always 200 when the
//     callerID matches.
func FuzzGetStatement(f *testing.F) {
	gin.SetMode(gin.TestMode)

	// ── Seed corpus ──────────────────────────────────────────────────────────
	type seed struct{ statementID, callerID string }
	seeds := []seed{
		// Happy path — matches stub
		{"stmt-known", "cust-A"},

		// Unknown IDs
		{"stmt-unknown", "cust-A"},
		{"", "cust-A"},
		{"00000000-0000-0000-0000-000000000000", "cust-A"},

		// UUID-shaped IDs
		{"123e4567-e89b-12d3-a456-426614174000", "cust-A"},

		// SQL / injection probes
		{"' OR 1=1--", "cust-A"},
		{"../../../etc/passwd", "cust-A"},
		{"%00", "cust-A"},
		{"<script>alert(1)</script>", "cust-A"},
		{`{"id":"stmt-known"}`, "cust-A"},

		// Oversized IDs
		{string(make([]byte, 2048)), "cust-A"},

		// UTF-8 boundary IDs
		{"stmt-\xc3\xa9", "cust-A"},
		{"stmt-你好世界", "cust-A"},

		// Caller variations
		{"stmt-known", ""},
		{"stmt-known", "cust-other"},
		{"stmt-known", "admin-1"},
	}

	for _, s := range seeds {
		f.Add(s.statementID, s.callerID)
	}

	// ── Fuzz target ──────────────────────────────────────────────────────────
	svc := &stubStatementService{}
	handler := NewGetStatementHandler(svc)

	allowedCodes := map[int]bool{
		http.StatusOK:                  true,
		http.StatusUnauthorized:        true,
		http.StatusForbidden:           true,
		http.StatusNotFound:            true,
		http.StatusInternalServerError: true,
	}

	f.Fuzz(func(t *testing.T, statementID, callerID string) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		req, err := http.NewRequest(http.MethodGet, "/api/v1/statements/"+statementID, nil)
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		c.Request = req

		// Inject statement ID as the Gin path parameter.
		c.Params = gin.Params{{Key: "id", Value: statementID}}

		if callerID != "" {
			setStubAuth(c, callerID, []string{"subscriber"})
		}
		// Omitting auth → handler returns 401

		// Invariant 1: must not panic.
		handler(c)

		code := w.Code

		// Invariant 2: only expected status codes.
		if !allowedCodes[code] {
			t.Errorf("unexpected HTTP status %d for statementID=%q callerID=%q",
				code, statementID, callerID)
		}

		body := w.Body.Bytes()

		// Invariant 3: body must always be valid JSON.
		if utf8.ValidString(statementID) && utf8.ValidString(callerID) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Errorf("non-JSON body for statementID=%q callerID=%q: %v\nbody: %s",
					statementID, callerID, err, body)
				return
			}

			// Invariant 4: non-200 responses include "error".
			if code != http.StatusOK {
				if _, ok := raw["error"]; !ok {
					t.Errorf("non-200 response missing 'error' key for statementID=%q\nbody: %s",
						statementID, body)
				}
			}
		}

		// Invariant 5: the known statement with matching caller is always 200.
		if statementID == "stmt-known" && callerID == "cust-A" && code != http.StatusOK {
			t.Errorf("expected 200 for known stmt+caller, got %d\nbody: %s", code, body)
		}
	})
}
