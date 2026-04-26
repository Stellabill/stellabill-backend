package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
)

// =============================================================================
// Test helpers
// =============================================================================

// setupAdminRouter builds a Gin router with audit middleware and all five admin
// routes wired to the supplied AdminHandler.  It returns the engine and the
// in-memory sink so tests can inspect emitted audit entries.
func setupAdminRouter(token string) (*gin.Engine, *audit.MemorySink) {
	gin.SetMode(gin.TestMode)
	sink := &audit.MemorySink{}
	logger := audit.NewLogger("test-secret", sink)

	r := gin.New()
	r.Use(audit.Middleware(logger))

	h := NewAdminHandler(token)
	r.POST("/api/admin/purge", h.PurgeCache)
	r.POST("/api/admin/users/ban", h.BanUser)
	r.POST("/api/admin/plans/update-price", h.UpdatePlanPrice)
	r.POST("/api/admin/subscriptions/reactivate", h.ReactivateSubscription)
	r.GET("/api/admin/audit-log", h.GetAuditLog)

	return r, sink
}

// lastEntry returns the most recently written audit entry in the sink.
// It panics if the sink is empty so a failing assertion surfaces clearly.
func lastEntry(sink *audit.MemorySink) audit.Entry {
	entries := sink.Entries()
	if len(entries) == 0 {
		panic("lastEntry: sink is empty")
	}
	return entries[len(entries)-1]
}

// jsonBody serialises v to a *bytes.Buffer suitable for use as an HTTP body.
func jsonBody(v interface{}) *bytes.Buffer {
	data, _ := json.Marshal(v)
	return bytes.NewBuffer(data)
}

// adminReq builds an HTTP request pre-loaded with the standard admin auth headers.
func adminReq(method, url string, body *bytes.Buffer, token, role, actor string) *http.Request {
	var req *http.Request
	if body != nil {
		req, _ = http.NewRequest(method, url, body)
	} else {
		req, _ = http.NewRequest(method, url, nil)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", token)
	if role != "" {
		req.Header.Set("X-Admin-Role", role)
	}
	if actor != "" {
		req.Header.Set("X-Admin-User", actor)
	}
	return req
}

// validUUID is a well-formed RFC-4122 UUID used across multiple test cases.
const validUUID = "550e8400-e29b-41d4-a716-446655440000"

// =============================================================================
// Original tests (preserved, updated to include X-Admin-Role)
// =============================================================================

func TestAdminPurgeSuccess(t *testing.T) {
	r, sink := setupAdminRouter("token")

	req := adminReq("POST", "/api/admin/purge?target=cache&attempt=2", nil, "token", "ops_admin", "root")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	entry := lastEntry(sink)
	if entry.Outcome != "success" {
		t.Fatalf("expected audit outcome 'success', got %q", entry.Outcome)
	}
	if entry.Target != "cache" {
		t.Fatalf("expected audit target 'cache', got %q", entry.Target)
	}
	if entry.Metadata["attempt"] != "2" {
		t.Fatalf("expected attempt metadata '2', got %q", entry.Metadata["attempt"])
	}
}

func TestAdminPurgePartialAndRetry(t *testing.T) {
	r, sink := setupAdminRouter("token")

	req := adminReq("POST", "/api/admin/purge?partial=1&attempt=3", nil, "token", "ops_admin", "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	entry := lastEntry(sink)
	if entry.Outcome != "partial" {
		t.Fatalf("expected audit outcome 'partial', got %q", entry.Outcome)
	}
	if entry.Metadata["attempt"] != "3" {
		t.Fatalf("expected attempt metadata '3', got %q", entry.Metadata["attempt"])
	}
}

func TestAdminPurgeDenied(t *testing.T) {
	r, sink := setupAdminRouter("token")

	req, _ := http.NewRequest("POST", "/api/admin/purge", nil)
	req.Header.Set("X-Admin-Token", "wrong-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// The very first audit entry should be the denied purge action.
	entries := sink.Entries()
	if len(entries) == 0 {
		t.Fatal("expected at least one audit entry")
	}
	first := entries[0]
	if first.Action != "admin_purge" {
		t.Fatalf("expected action 'admin_purge', got %q", first.Action)
	}
	if first.Outcome != "denied" {
		t.Fatalf("expected outcome 'denied', got %q", first.Outcome)
	}
}

func TestAdminDefaultToken(t *testing.T) {
	r, _ := setupAdminRouter("") // empty → uses "change-me-admin-token"

	req := adminReq("POST", "/api/admin/purge", nil, "change-me-admin-token", "super_admin", "")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with default token, got %d: %s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// RBAC table tests
// =============================================================================

func TestAdminPurgeRBAC(t *testing.T) {
	cases := []struct {
		role     string
		wantCode int
	}{
		{"super_admin", http.StatusOK},
		{"ops_admin", http.StatusOK},
		{"billing_admin", http.StatusForbidden},
		{"read_only_admin", http.StatusForbidden},
		{"", http.StatusForbidden},
		{"unknown_role", http.StatusForbidden},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("role=%q", tc.role), func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("POST", "/api/admin/purge", nil, "tok", tc.role, "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("role %q: expected %d, got %d: %s", tc.role, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminBanUserRBAC(t *testing.T) {
	cases := []struct {
		role     string
		wantCode int
	}{
		{"super_admin", http.StatusOK},
		{"ops_admin", http.StatusOK},
		{"billing_admin", http.StatusForbidden},
		{"read_only_admin", http.StatusForbidden},
		{"", http.StatusForbidden},
		{"unknown_role", http.StatusForbidden},
	}

	body := jsonBody(map[string]string{
		"user_id": validUUID,
		"reason":  "violation of ToS",
	})

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("role=%q", tc.role), func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			b := bytes.NewBuffer(body.Bytes())
			req := adminReq("POST", "/api/admin/users/ban", b, "tok", tc.role, "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("role %q: expected %d, got %d: %s", tc.role, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminUpdatePlanPriceRBAC(t *testing.T) {
	cases := []struct {
		role     string
		wantCode int
	}{
		{"super_admin", http.StatusOK},
		{"billing_admin", http.StatusOK},
		{"ops_admin", http.StatusForbidden},
		{"read_only_admin", http.StatusForbidden},
		{"", http.StatusForbidden},
		{"unknown_role", http.StatusForbidden},
	}

	body := jsonBody(map[string]string{
		"plan_id":   validUUID,
		"new_price": "29.99",
		"currency":  "USD",
	})

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("role=%q", tc.role), func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			b := bytes.NewBuffer(body.Bytes())
			req := adminReq("POST", "/api/admin/plans/update-price", b, "tok", tc.role, "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("role %q: expected %d, got %d: %s", tc.role, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminReactivateSubscriptionRBAC(t *testing.T) {
	cases := []struct {
		role     string
		wantCode int
	}{
		{"super_admin", http.StatusOK},
		{"billing_admin", http.StatusOK},
		{"ops_admin", http.StatusForbidden},
		{"read_only_admin", http.StatusForbidden},
		{"", http.StatusForbidden},
		{"unknown_role", http.StatusForbidden},
	}

	body := jsonBody(map[string]string{
		"subscription_id": validUUID,
	})

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("role=%q", tc.role), func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			b := bytes.NewBuffer(body.Bytes())
			req := adminReq("POST", "/api/admin/subscriptions/reactivate", b, "tok", tc.role, "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("role %q: expected %d, got %d: %s", tc.role, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAdminGetAuditLogRBAC(t *testing.T) {
	// All four valid roles must be able to read the audit log.
	cases := []struct {
		role     string
		wantCode int
	}{
		{"super_admin", http.StatusOK},
		{"billing_admin", http.StatusOK},
		{"ops_admin", http.StatusOK},
		{"read_only_admin", http.StatusOK},
		{"", http.StatusForbidden},
		{"unknown_role", http.StatusForbidden},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(fmt.Sprintf("role=%q", tc.role), func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("GET", "/api/admin/audit-log", nil, "tok", tc.role, "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("role %q: expected %d, got %d: %s", tc.role, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// =============================================================================
// Privilege-escalation prevention
// =============================================================================

// TestAdminPrivilegeEscalation verifies that no role can exceed its granted
// permissions by attempting every cross-domain operation.
func TestAdminPrivilegeEscalation(t *testing.T) {
	validBanBody := jsonBody(map[string]string{"user_id": validUUID, "reason": "test"})
	validPriceBody := jsonBody(map[string]string{"plan_id": validUUID, "new_price": "9.99", "currency": "USD"})
	validReactivateBody := jsonBody(map[string]string{"subscription_id": validUUID})

	cases := []struct {
		name   string
		role   string
		method string
		url    string
		body   *bytes.Buffer
	}{
		// ops_admin must not access billing operations
		{"ops_admin_cannot_update_plan_price", "ops_admin", "POST", "/api/admin/plans/update-price", bytes.NewBuffer(validPriceBody.Bytes())},
		{"ops_admin_cannot_reactivate_sub", "ops_admin", "POST", "/api/admin/subscriptions/reactivate", bytes.NewBuffer(validReactivateBody.Bytes())},

		// billing_admin must not access operational actions
		{"billing_admin_cannot_purge", "billing_admin", "POST", "/api/admin/purge", nil},
		{"billing_admin_cannot_ban_user", "billing_admin", "POST", "/api/admin/users/ban", bytes.NewBuffer(validBanBody.Bytes())},

		// read_only_admin must not perform any mutating action
		{"read_only_cannot_purge", "read_only_admin", "POST", "/api/admin/purge", nil},
		{"read_only_cannot_ban_user", "read_only_admin", "POST", "/api/admin/users/ban", bytes.NewBuffer(validBanBody.Bytes())},
		{"read_only_cannot_update_plan_price", "read_only_admin", "POST", "/api/admin/plans/update-price", bytes.NewBuffer(validPriceBody.Bytes())},
		{"read_only_cannot_reactivate_sub", "read_only_admin", "POST", "/api/admin/subscriptions/reactivate", bytes.NewBuffer(validReactivateBody.Bytes())},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, sink := setupAdminRouter("tok")
			req := adminReq(tc.method, tc.url, tc.body, "tok", tc.role, "attacker")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s: expected 403, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}

			// The denied audit event must record the attempted privilege escalation.
			entry := lastEntry(sink)
			if entry.Outcome != "denied" {
				t.Fatalf("%s: expected audit outcome 'denied', got %q", tc.name, entry.Outcome)
			}
		})
	}
}

// =============================================================================
// Input-validation tests – PurgeCache
// =============================================================================

func TestAdminPurgeValidation(t *testing.T) {
	longTarget := strings.Repeat("a", 201)

	cases := []struct {
		name     string
		url      string
		wantCode int
	}{
		{"target_with_sql_injection", "/api/admin/purge?target=cache%27+OR+1%3D1", http.StatusBadRequest},
		{"target_with_xss", "/api/admin/purge?target=%3Cscript%3E", http.StatusBadRequest},
		{"target_too_long", "/api/admin/purge?target=" + longTarget, http.StatusBadRequest},
		{"target_with_space", "/api/admin/purge?target=cache+name", http.StatusBadRequest},
		{"attempt_not_integer", "/api/admin/purge?attempt=abc", http.StatusBadRequest},
		{"attempt_zero", "/api/admin/purge?attempt=0", http.StatusBadRequest},
		{"attempt_above_max", "/api/admin/purge?attempt=11", http.StatusBadRequest},
		{"attempt_negative", "/api/admin/purge?attempt=-1", http.StatusBadRequest},
		{"attempt_at_min", "/api/admin/purge?attempt=1", http.StatusOK},
		{"attempt_at_max", "/api/admin/purge?attempt=10", http.StatusOK},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("POST", tc.url, nil, "tok", "ops_admin", "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s: expected %d, got %d: %s", tc.name, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// =============================================================================
// Input-validation tests – BanUser
// =============================================================================

func TestAdminBanUserValidation(t *testing.T) {
	longReason := strings.Repeat("x", 501)

	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"missing_user_id", `{"reason":"test"}`, http.StatusBadRequest},
		{"missing_reason", `{"user_id":"` + validUUID + `"}`, http.StatusBadRequest},
		{"invalid_uuid_format", `{"user_id":"not-a-uuid","reason":"test"}`, http.StatusBadRequest},
		{"uuid_too_short", `{"user_id":"550e8400-e29b-41d4","reason":"test"}`, http.StatusBadRequest},
		{"reason_too_long", `{"user_id":"` + validUUID + `","reason":"` + longReason + `"}`, http.StatusBadRequest},
		{"malformed_json", `{bad json}`, http.StatusBadRequest},
		{"empty_body", `{}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("POST", "/api/admin/users/ban", bytes.NewBufferString(tc.body), "tok", "ops_admin", "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s: expected %d, got %d: %s", tc.name, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// =============================================================================
// Input-validation tests – UpdatePlanPrice
// =============================================================================

func TestAdminUpdatePlanPriceValidation(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"missing_plan_id", `{"new_price":"9.99","currency":"USD"}`, http.StatusBadRequest},
		{"invalid_uuid_plan_id", `{"plan_id":"not-uuid","new_price":"9.99","currency":"USD"}`, http.StatusBadRequest},
		{"missing_new_price", `{"plan_id":"` + validUUID + `","currency":"USD"}`, http.StatusBadRequest},
		{"negative_price", `{"plan_id":"` + validUUID + `","new_price":"-5","currency":"USD"}`, http.StatusBadRequest},
		{"alpha_price", `{"plan_id":"` + validUUID + `","new_price":"abc","currency":"USD"}`, http.StatusBadRequest},
		{"price_too_many_decimals", `{"plan_id":"` + validUUID + `","new_price":"1.234","currency":"USD"}`, http.StatusBadRequest},
		{"price_too_many_digits", `{"plan_id":"` + validUUID + `","new_price":"1234567.89","currency":"USD"}`, http.StatusBadRequest},
		{"missing_currency", `{"plan_id":"` + validUUID + `","new_price":"9.99"}`, http.StatusBadRequest},
		{"currency_too_short", `{"plan_id":"` + validUUID + `","new_price":"9.99","currency":"US"}`, http.StatusBadRequest},
		{"currency_too_long", `{"plan_id":"` + validUUID + `","new_price":"9.99","currency":"USDD"}`, http.StatusBadRequest},
		{"currency_with_digits", `{"plan_id":"` + validUUID + `","new_price":"9.99","currency":"U5D"}`, http.StatusBadRequest},
		{"malformed_json", `{bad json}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("POST", "/api/admin/plans/update-price", bytes.NewBufferString(tc.body), "tok", "billing_admin", "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s: expected %d, got %d: %s", tc.name, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// =============================================================================
// Input-validation tests – ReactivateSubscription
// =============================================================================

func TestAdminReactivateSubscriptionValidation(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"missing_subscription_id", `{}`, http.StatusBadRequest},
		{"invalid_uuid", `{"subscription_id":"not-a-uuid"}`, http.StatusBadRequest},
		{"malformed_json", `{bad json}`, http.StatusBadRequest},
		{"empty_string_id", `{"subscription_id":""}`, http.StatusBadRequest},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("POST", "/api/admin/subscriptions/reactivate", bytes.NewBufferString(tc.body), "tok", "billing_admin", "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s: expected %d, got %d: %s", tc.name, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// =============================================================================
// Input-validation tests – GetAuditLog
// =============================================================================

func TestAdminGetAuditLogValidation(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		wantCode int
	}{
		{"limit_not_integer", "/api/admin/audit-log?limit=abc", http.StatusBadRequest},
		{"limit_zero", "/api/admin/audit-log?limit=0", http.StatusBadRequest},
		{"limit_too_high", "/api/admin/audit-log?limit=501", http.StatusBadRequest},
		{"limit_negative", "/api/admin/audit-log?limit=-1", http.StatusBadRequest},
		{"limit_at_min", "/api/admin/audit-log?limit=1", http.StatusOK},
		{"limit_at_max", "/api/admin/audit-log?limit=500", http.StatusOK},
		{"default_limit", "/api/admin/audit-log", http.StatusOK},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("GET", tc.url, nil, "tok", "read_only_admin", "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s: expected %d, got %d: %s", tc.name, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// =============================================================================
// Success tests for the four new actions
// =============================================================================

func TestAdminBanUserSuccess(t *testing.T) {
	r, sink := setupAdminRouter("tok")

	body := jsonBody(map[string]string{
		"user_id": validUUID,
		"reason":  "repeated ToS violations",
	})
	req := adminReq("POST", "/api/admin/users/ban", body, "tok", "ops_admin", "ops-admin-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if resp["status"] != "banned" {
		t.Fatalf("expected status 'banned', got %v", resp["status"])
	}
	if resp["user_id"] != validUUID {
		t.Fatalf("expected user_id %q, got %v", validUUID, resp["user_id"])
	}

	entry := lastEntry(sink)
	if entry.Action != "admin_ban_user" || entry.Outcome != "success" {
		t.Fatalf("unexpected audit entry: action=%q outcome=%q", entry.Action, entry.Outcome)
	}
}

func TestAdminUpdatePlanPriceSuccess(t *testing.T) {
	r, sink := setupAdminRouter("tok")

	body := jsonBody(map[string]string{
		"plan_id":   validUUID,
		"new_price": "49.99",
		"currency":  "eur", // lowercase – handler must uppercase it
	})
	req := adminReq("POST", "/api/admin/plans/update-price", body, "tok", "billing_admin", "billing-admin-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if resp["status"] != "updated" {
		t.Fatalf("expected status 'updated', got %v", resp["status"])
	}
	if resp["currency"] != "EUR" {
		t.Fatalf("expected currency 'EUR' (uppercased), got %v", resp["currency"])
	}

	entry := lastEntry(sink)
	if entry.Action != "admin_update_plan_price" || entry.Outcome != "success" {
		t.Fatalf("unexpected audit entry: action=%q outcome=%q", entry.Action, entry.Outcome)
	}
	if entry.Metadata["currency"] != "EUR" {
		t.Fatalf("expected currency 'EUR' in audit metadata, got %q", entry.Metadata["currency"])
	}
}

func TestAdminReactivateSubscriptionSuccess(t *testing.T) {
	r, sink := setupAdminRouter("tok")

	body := jsonBody(map[string]string{
		"subscription_id": validUUID,
	})
	req := adminReq("POST", "/api/admin/subscriptions/reactivate", body, "tok", "billing_admin", "billing-admin-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if resp["status"] != "reactivated" {
		t.Fatalf("expected status 'reactivated', got %v", resp["status"])
	}
	if resp["subscription_id"] != validUUID {
		t.Fatalf("expected subscription_id %q, got %v", validUUID, resp["subscription_id"])
	}

	entry := lastEntry(sink)
	if entry.Action != "admin_reactivate_sub" || entry.Outcome != "success" {
		t.Fatalf("unexpected audit entry: action=%q outcome=%q", entry.Action, entry.Outcome)
	}
}

func TestAdminGetAuditLogSuccess(t *testing.T) {
	r, sink := setupAdminRouter("tok")

	req := adminReq("GET", "/api/admin/audit-log", nil, "tok", "read_only_admin", "readonly-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("could not parse response: %v", err)
	}
	if _, ok := resp["entries"]; !ok {
		t.Fatal("expected 'entries' key in response")
	}
	// Default limit is 50.
	if resp["limit"] != float64(50) {
		t.Fatalf("expected default limit 50, got %v", resp["limit"])
	}

	entry := lastEntry(sink)
	if entry.Action != "admin_get_audit_log" || entry.Outcome != "success" {
		t.Fatalf("unexpected audit entry: action=%q outcome=%q", entry.Action, entry.Outcome)
	}
	if entry.Metadata["limit"] != "50" {
		t.Fatalf("expected limit '50' in audit metadata, got %q", entry.Metadata["limit"])
	}
}

// =============================================================================
// Audit field completeness
// =============================================================================

// TestAdminAuditEnrichedFields ensures that every successful admin audit event
// carries the mandatory enriched fields: actor, role, request_id, user_agent.
func TestAdminAuditEnrichedFields(t *testing.T) {
	r, sink := setupAdminRouter("tok")

	req := adminReq("POST", "/api/admin/purge?target=billing-cache", nil, "tok", "ops_admin", "alice")
	req.Header.Set("X-Request-ID", "req-xyz-123")
	req.Header.Set("User-Agent", "test-agent/1.0")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	entry := lastEntry(sink)

	if entry.Metadata["actor"] != "alice" {
		t.Errorf("expected actor 'alice', got %q", entry.Metadata["actor"])
	}
	if entry.Metadata["role"] != "ops_admin" {
		t.Errorf("expected role 'ops_admin', got %q", entry.Metadata["role"])
	}
	if entry.Metadata["request_id"] != "req-xyz-123" {
		t.Errorf("expected request_id 'req-xyz-123', got %q", entry.Metadata["request_id"])
	}
	if entry.Metadata["user_agent"] != "test-agent/1.0" {
		t.Errorf("expected user_agent 'test-agent/1.0', got %q", entry.Metadata["user_agent"])
	}
}

// TestAdminAuditEnrichedFieldsAllActions verifies every handler emits the
// enriched fields, not just PurgeCache.
func TestAdminAuditEnrichedFieldsAllActions(t *testing.T) {
	subUUID := validUUID

	type action struct {
		name   string
		method string
		url    string
		body   *bytes.Buffer
		role   string
		action string
	}

	actions := []action{
		{
			"purge", "POST", "/api/admin/purge", nil,
			"ops_admin", "admin_purge",
		},
		{
			"ban_user", "POST", "/api/admin/users/ban",
			jsonBody(map[string]string{"user_id": subUUID, "reason": "test"}),
			"ops_admin", "admin_ban_user",
		},
		{
			"update_plan_price", "POST", "/api/admin/plans/update-price",
			jsonBody(map[string]string{"plan_id": subUUID, "new_price": "9.99", "currency": "USD"}),
			"billing_admin", "admin_update_plan_price",
		},
		{
			"reactivate_sub", "POST", "/api/admin/subscriptions/reactivate",
			jsonBody(map[string]string{"subscription_id": subUUID}),
			"billing_admin", "admin_reactivate_sub",
		},
		{
			"get_audit_log", "GET", "/api/admin/audit-log", nil,
			"read_only_admin", "admin_get_audit_log",
		},
	}

	for _, a := range actions {
		a := a
		t.Run(a.name, func(t *testing.T) {
			r, sink := setupAdminRouter("tok")
			var b *bytes.Buffer
			if a.body != nil {
				b = bytes.NewBuffer(a.body.Bytes())
			}
			req := adminReq(a.method, a.url, b, "tok", a.role, "enriched-actor")
			req.Header.Set("X-Request-ID", "rid-"+a.name)
			req.Header.Set("User-Agent", "ua-"+a.name)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code >= 400 {
				t.Fatalf("unexpected error %d: %s", rec.Code, rec.Body.String())
			}

			entry := lastEntry(sink)
			if entry.Action != a.action {
				t.Errorf("expected action %q, got %q", a.action, entry.Action)
			}
			if entry.Metadata["actor"] != "enriched-actor" {
				t.Errorf("missing actor in metadata: %v", entry.Metadata)
			}
			if entry.Metadata["role"] != a.role {
				t.Errorf("missing role in metadata: %v", entry.Metadata)
			}
			if entry.Metadata["request_id"] != "rid-"+a.name {
				t.Errorf("missing request_id in metadata: %v", entry.Metadata)
			}
			if entry.Metadata["user_agent"] != "ua-"+a.name {
				t.Errorf("missing user_agent in metadata: %v", entry.Metadata)
			}
		})
	}
}

// =============================================================================
// Actor validation
// =============================================================================

func TestAdminActorValidation(t *testing.T) {
	longActor := strings.Repeat("a", 101)

	cases := []struct {
		name     string
		actor    string
		wantCode int
	}{
		{"xss_in_actor", "<script>alert(1)</script>", http.StatusBadRequest},
		{"sql_injection_in_actor", "' OR 1=1 --", http.StatusBadRequest},
		{"actor_too_long", longActor, http.StatusBadRequest},
		{"actor_with_spaces", "alice bob", http.StatusBadRequest},
		{"actor_with_percent", "alice%20bob", http.StatusBadRequest},
		{"valid_actor_simple", "alice", http.StatusOK},
		{"valid_actor_with_hyphens", "ops-admin-1", http.StatusOK},
		{"valid_actor_with_dots", "admin.user", http.StatusOK},
		{"valid_actor_with_underscores", "admin_user_1", http.StatusOK},
		{"valid_actor_exactly_100_chars", strings.Repeat("a", 100), http.StatusOK},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("POST", "/api/admin/purge", nil, "tok", "ops_admin", tc.actor)
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Fatalf("actor %q: expected %d, got %d: %s", tc.actor, tc.wantCode, rec.Code, rec.Body.String())
			}
		})
	}
}

// =============================================================================
// Audit hash-chain integrity
// =============================================================================

// TestAdminAuditHashChain verifies the tamper-evident hash chain holds across
// multiple successive admin actions on the same audit logger instance.
func TestAdminAuditHashChain(t *testing.T) {
	r, sink := setupAdminRouter("tok")

	// First call: purge
	req1 := adminReq("POST", "/api/admin/purge", nil, "tok", "ops_admin", "admin")
	r.ServeHTTP(httptest.NewRecorder(), req1)

	// Second call: ban user
	req2 := adminReq("POST", "/api/admin/users/ban",
		jsonBody(map[string]string{"user_id": validUUID, "reason": "chain test"}),
		"tok", "ops_admin", "admin")
	r.ServeHTTP(httptest.NewRecorder(), req2)

	// Third call: get audit log
	req3 := adminReq("GET", "/api/admin/audit-log", nil, "tok", "read_only_admin", "admin")
	r.ServeHTTP(httptest.NewRecorder(), req3)

	entries := sink.Entries()
	// There may be additional auth_failure entries from the middleware post-hook;
	// filter to only the admin action entries.
	var adminEntries []audit.Entry
	for _, e := range entries {
		if e.Action == "admin_purge" || e.Action == "admin_ban_user" || e.Action == "admin_get_audit_log" {
			adminEntries = append(adminEntries, e)
		}
	}

	if len(adminEntries) < 3 {
		t.Fatalf("expected at least 3 admin audit entries, got %d (total: %d)", len(adminEntries), len(entries))
	}

	// Verify chain: entry[n].PrevHash == entry[n-1].Hash
	for i := 1; i < len(adminEntries); i++ {
		if adminEntries[i].PrevHash != adminEntries[i-1].Hash {
			t.Errorf("hash chain broken between entry %d and %d: prev=%q expected=%q",
				i-1, i, adminEntries[i].PrevHash, adminEntries[i-1].Hash)
		}
		if adminEntries[i].Hash == "" {
			t.Errorf("entry %d has empty hash", i)
		}
	}
}

// =============================================================================
// Audit event emitted on validation failure
// =============================================================================

// TestAdminAuditOnValidationFailure ensures that when a handler rejects a
// request for input-validation reasons, it still emits a "denied" audit entry
// so that malformed or probing requests are traceable.
func TestAdminAuditOnValidationFailure(t *testing.T) {
	t.Run("invalid_target_emits_denied_audit", func(t *testing.T) {
		r, sink := setupAdminRouter("tok")
		req := adminReq("POST", "/api/admin/purge?target=<bad>", nil, "tok", "ops_admin", "admin")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		entry := lastEntry(sink)
		if entry.Outcome != "denied" {
			t.Fatalf("expected audit outcome 'denied', got %q", entry.Outcome)
		}
	})

	t.Run("invalid_uuid_ban_emits_denied_audit", func(t *testing.T) {
		r, sink := setupAdminRouter("tok")
		body := jsonBody(map[string]string{"user_id": "not-a-uuid", "reason": "test"})
		req := adminReq("POST", "/api/admin/users/ban", body, "tok", "ops_admin", "admin")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		entry := lastEntry(sink)
		if entry.Outcome != "denied" {
			t.Fatalf("expected audit outcome 'denied', got %q", entry.Outcome)
		}
	})

	t.Run("invalid_price_emits_denied_audit", func(t *testing.T) {
		r, sink := setupAdminRouter("tok")
		body := jsonBody(map[string]string{
			"plan_id": validUUID, "new_price": "not-a-price", "currency": "USD",
		})
		req := adminReq("POST", "/api/admin/plans/update-price", body, "tok", "billing_admin", "admin")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		entry := lastEntry(sink)
		if entry.Outcome != "denied" {
			t.Fatalf("expected audit outcome 'denied', got %q", entry.Outcome)
		}
	})

	t.Run("invalid_limit_emits_denied_audit", func(t *testing.T) {
		r, sink := setupAdminRouter("tok")
		req := adminReq("GET", "/api/admin/audit-log?limit=9999", nil, "tok", "read_only_admin", "admin")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
		entry := lastEntry(sink)
		if entry.Outcome != "denied" {
			t.Fatalf("expected audit outcome 'denied', got %q", entry.Outcome)
		}
	})
}

// =============================================================================
// Standardised error response shape
// =============================================================================

// TestAdminErrorResponseShape confirms that error responses carry the
// standardised ErrorEnvelope with a non-empty "code" field.
func TestAdminErrorResponseShape(t *testing.T) {
	cases := []struct {
		name         string
		req          func(r *gin.Engine) *httptest.ResponseRecorder
		wantCode     int
		wantErrCode  string
	}{
		{
			name: "invalid_token_returns_UNAUTHORIZED",
			req: func(r *gin.Engine) *httptest.ResponseRecorder {
				req, _ := http.NewRequest("POST", "/api/admin/purge", nil)
				req.Header.Set("X-Admin-Token", "bad")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				return rec
			},
			wantCode:    http.StatusUnauthorized,
			wantErrCode: "UNAUTHORIZED",
		},
		{
			name: "wrong_role_returns_FORBIDDEN",
			req: func(r *gin.Engine) *httptest.ResponseRecorder {
				req := adminReq("POST", "/api/admin/purge", nil, "tok", "billing_admin", "admin")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				return rec
			},
			wantCode:    http.StatusForbidden,
			wantErrCode: "FORBIDDEN",
		},
		{
			name: "invalid_target_returns_VALIDATION_FAILED",
			req: func(r *gin.Engine) *httptest.ResponseRecorder {
				req := adminReq("POST", "/api/admin/purge?target=<xss>", nil, "tok", "ops_admin", "admin")
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, req)
				return rec
			},
			wantCode:    http.StatusBadRequest,
			wantErrCode: "VALIDATION_FAILED",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			rec := tc.req(r)

			if rec.Code != tc.wantCode {
				t.Fatalf("expected HTTP %d, got %d: %s", tc.wantCode, rec.Code, rec.Body.String())
			}

			var envelope struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("could not parse error response: %v – body: %s", err, rec.Body.String())
			}
			if envelope.Code != tc.wantErrCode {
				t.Fatalf("expected error code %q, got %q (message: %q)", tc.wantErrCode, envelope.Code, envelope.Message)
			}
			if envelope.Message == "" {
				t.Error("error response must include a non-empty message")
			}
		})
	}
}

// =============================================================================
// Unknown role must never silently pass
// =============================================================================

// TestAdminUnknownRoleAlwaysDenied is a targeted security test confirming that
// a crafted role value cannot slip past the validRoles check.
func TestAdminUnknownRoleAlwaysDenied(t *testing.T) {
	crafted := []string{
		"SUPER_ADMIN",           // wrong case
		"Super_Admin",           // mixed case
		"super_admin ",          // trailing space (trimmed by handler but still invalid after trim? no – handler trims it, so this actually equals super_admin. Skip.)
		"super_admin\x00",       // null byte
		"root",                  // unix-style
		"administrator",         // windows-style
		"*",                     // wildcard attempt
		"super_admin,ops_admin", // injection attempt
	}

	for _, role := range crafted {
		role := role
		// Skip the case where trimming makes it a valid role.
		trimmed := strings.TrimSpace(role)
		if _, valid := validRoles[AdminRole(trimmed)]; valid {
			continue
		}
		t.Run(fmt.Sprintf("role=%q", role), func(t *testing.T) {
			r, _ := setupAdminRouter("tok")
			req := adminReq("POST", "/api/admin/purge", nil, "tok", role, "admin")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("crafted role %q should be denied (403), got %d: %s", role, rec.Code, rec.Body.String())
			}
		})
	}
}
