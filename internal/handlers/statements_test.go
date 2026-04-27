package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

// ── mock ─────────────────────────────────────────────────────────────────────

type mockStatementService struct {
	detail       *service.StatementDetail
	listDetail   *service.ListStatementsDetail
	warnings     []string
	count        int
	err          error
	capturedQ    repository.StatementQuery
	capturedCust string
}

func (m *mockStatementService) GetDetail(_ context.Context, _ string, _ []string, _ string) (*service.StatementDetail, []string, error) {
	return m.detail, m.warnings, m.err
}

func (m *mockStatementService) ListByCustomer(_ context.Context, _ string, _ []string, customerID string, q repository.StatementQuery) (*service.ListStatementsDetail, int, []string, error) {
	m.capturedQ = q
	m.capturedCust = customerID
	return m.listDetail, m.count, m.warnings, m.err
}

// ── helpers ──────────────────────────────────────────────────────────────────

func stmtRouter(svc service.StatementService, setCallerID bool) *gin.Engine {
	return stmtRouterWithAuth(svc, setCallerID, "cust-1", []string{"customer"})
}

func stmtRouterWithAuth(svc service.StatementService, setCallerID bool, callerID string, roles []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if setCallerID {
		r.Use(func(c *gin.Context) {
			c.Set("callerID", callerID)
			c.Set("roles", roles)
			c.Next()
		})
	}
	r.GET("/api/statements/:id", NewGetStatementHandler(svc))
	r.GET("/api/statements", NewListStatementsHandler(svc))
	return r
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return body
}

// ── GetStatement tests ───────────────────────────────────────────────────────

func TestGetStatement_MissingCallerID_Returns401(t *testing.T) {
	r := stmtRouter(&mockStatementService{}, false)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["error"] != "unauthorized" {
		t.Errorf("unexpected error: %v", body["error"])
	}
}

func TestGetStatement_EmptyID_Returns400(t *testing.T) {
	r := stmtRouter(&mockStatementService{}, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements/%20", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["error"] != "statement id required" {
		t.Errorf("unexpected error: %v", body["error"])
	}
}

func TestGetStatement_ErrNotFound_Returns404(t *testing.T) {
	svc := &mockStatementService{err: service.ErrNotFound}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-missing", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["error"] != "statement not found" {
		t.Errorf("unexpected error: %v", body["error"])
	}
}

func TestGetStatement_ErrDeleted_Returns410(t *testing.T) {
	svc := &mockStatementService{err: service.ErrDeleted}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-del", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Fatalf("expected 410, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["error"] != "statement has been deleted" {
		t.Errorf("unexpected error: %v", body["error"])
	}
}

func TestGetStatement_ErrForbidden_Returns403(t *testing.T) {
	svc := &mockStatementService{err: service.ErrForbidden}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["error"] != "forbidden" {
		t.Errorf("unexpected error: %v", body["error"])
	}
}

func TestGetStatement_UnknownError_Returns500(t *testing.T) {
	svc := &mockStatementService{err: errors.New("db connection lost")}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
	body := decodeBody(t, w)
	if body["error"] != "internal error" {
		t.Errorf("unexpected error: %v", body["error"])
	}
}

func TestGetStatement_HappyPath_Returns200WithEnvelope(t *testing.T) {
	detail := &service.StatementDetail{
		ID:             "stmt-1",
		SubscriptionID: "sub-1",
		Customer:       "cust-1",
		PeriodStart:    "2024-01-01T00:00:00Z",
		PeriodEnd:      "2024-02-01T00:00:00Z",
		IssuedAt:       "2024-02-02T00:00:00Z",
		TotalAmount:    "2999",
		Currency:       "USD",
		Kind:           "invoice",
		Status:         "paid",
	}
	svc := &mockStatementService{detail: detail}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %q", ct)
	}

	envelope := decodeBody(t, w)
	if envelope["api_version"] != "2025-01-01" {
		t.Errorf("expected api_version=2025-01-01, got %v", envelope["api_version"])
	}

	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data field to be an object")
	}
	if data["id"] != "stmt-1" {
		t.Errorf("expected data.id=stmt-1, got %v", data["id"])
	}
	if data["subscription_id"] != "sub-1" {
		t.Errorf("expected data.subscription_id=sub-1, got %v", data["subscription_id"])
	}
	if data["customer"] != "cust-1" {
		t.Errorf("expected data.customer=cust-1, got %v", data["customer"])
	}
	if data["kind"] != "invoice" {
		t.Errorf("expected data.kind=invoice, got %v", data["kind"])
	}
	if data["status"] != "paid" {
		t.Errorf("expected data.status=paid, got %v", data["status"])
	}
	if data["total_amount"] != "2999" {
		t.Errorf("expected data.total_amount=2999, got %v", data["total_amount"])
	}
	if data["currency"] != "USD" {
		t.Errorf("expected data.currency=USD, got %v", data["currency"])
	}
}

func TestGetStatement_HappyPath_WarningsIncluded(t *testing.T) {
	detail := &service.StatementDetail{ID: "stmt-1"}
	svc := &mockStatementService{detail: detail, warnings: []string{"subscription missing"}}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements/stmt-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	envelope := decodeBody(t, w)
	warns, ok := envelope["warnings"].([]interface{})
	if !ok {
		t.Fatal("expected warnings to be an array")
	}
	if len(warns) != 1 || warns[0] != "subscription missing" {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

// ── ListStatements tests ─────────────────────────────────────────────────────

func TestListStatements_MissingCallerID_Returns401(t *testing.T) {
	r := stmtRouter(&mockStatementService{}, false)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestListStatements_ErrForbidden_Returns403(t *testing.T) {
	svc := &mockStatementService{err: service.ErrForbidden}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestListStatements_UnknownError_Returns500(t *testing.T) {
	svc := &mockStatementService{err: errors.New("db down")}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListStatements_HappyPath_Returns200WithPagination(t *testing.T) {
	stmts := []*service.StatementDetail{
		{ID: "stmt-1", Kind: "invoice", Status: "paid"},
		{ID: "stmt-2", Kind: "invoice", Status: "pending"},
	}
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{Statements: stmts},
		count:      2,
	}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements?page=1&page_size=10", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if ct != "application/json; charset=utf-8" {
		t.Errorf("unexpected Content-Type: %q", ct)
	}

	envelope := decodeBody(t, w)
	if envelope["api_version"] != "2025-01-01" {
		t.Errorf("expected api_version=2025-01-01, got %v", envelope["api_version"])
	}

	pag, ok := envelope["pagination"].(map[string]interface{})
	if !ok {
		t.Fatal("expected pagination field")
	}
	// page parameter is now ignored/removed, but let's check what's there
	if pag["page"] != nil {
		t.Errorf("expected page to be nil, got %v", pag["page"])
	}
	if pag["page_size"] != nil {
		t.Errorf("expected page_size to be nil, got %v", pag["page_size"])
	}
	if pag["total_count"] != float64(2) {
		t.Errorf("expected count=2, got %v", pag["total_count"])
	}

	data, ok := envelope["data"].(map[string]interface{})
	if !ok {
		t.Fatal("expected data to be an object")
	}
	statements, ok := data["statements"].([]interface{})
	if !ok {
		t.Fatal("expected data.statements to be an array")
	}
	if len(statements) != 2 {
		t.Errorf("expected 2 statements, got %d", len(statements))
	}
}

func TestListStatements_HasMore(t *testing.T) {
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{
			Statements: []*service.StatementDetail{
				{ID: "stmt-1"},
				{ID: "stmt-2"},
			},
		},
		count: 2,
	}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	// Request with limit=2, so len(Statements) == limit => HasMore=true
	req, _ := http.NewRequest(http.MethodGet, "/api/statements?limit=2", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	envelope := decodeBody(t, w)
	pag := envelope["pagination"].(map[string]interface{})
	if pag["has_more"] != true {
		t.Errorf("expected has_more=true, got %v", pag["has_more"])
	}
	if pag["next_cursor"] != "stmt-2" {
		t.Errorf("expected next_cursor=stmt-2, got %v", pag["next_cursor"])
	}
}

func TestListStatements_InternalError_Returns500(t *testing.T) {
	svc := &mockStatementService{err: errors.New("something went wrong")}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListStatements_DefaultPagination(t *testing.T) {
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
	}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	// No page or page_size params — should default to page=1, page_size=10
	req, _ := http.NewRequest(http.MethodGet, "/api/statements", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	envelope := decodeBody(t, w)
	pag := envelope["pagination"].(map[string]interface{})
	if pag["has_more"] != false {
		t.Errorf("expected has_more=false, got %v", pag["has_more"])
	}
}

func TestListStatements_QueryFiltersPassedToService(t *testing.T) {
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
	}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements?subscription_id=sub-1&kind=invoice&status=paid&start_after=2024-01-01&end_before=2024-12-31&page=2&page_size=5", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	q := svc.capturedQ
	if q.SubscriptionID != "sub-1" {
		t.Errorf("SubscriptionID: got %q, want sub-1", q.SubscriptionID)
	}
	if q.Kind != "invoice" {
		t.Errorf("Kind: got %q, want invoice", q.Kind)
	}
	if q.Status != "paid" {
		t.Errorf("Status: got %q, want paid", q.Status)
	}
	if q.StartAfter != "2024-01-01" {
		t.Errorf("StartAfter: got %q, want 2024-01-01", q.StartAfter)
	}
	if q.EndBefore != "2024-12-31" {
		t.Errorf("EndBefore: got %q, want 2024-12-31", q.EndBefore)
	}
	if q.Limit != 5 {
		t.Errorf("Limit: got %d, want 5", q.Limit)
	}
}

func TestListStatements_InvalidPageParams_Ignored(t *testing.T) {
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
	}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements?page=abc&page_size=xyz", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Non-numeric values should be ignored (stay at zero).
	q := svc.capturedQ
	if q.Limit != 0 {
		t.Errorf("Limit: got %d, want 0 (unparsed)", q.Limit)
	}

	envelope := decodeBody(t, w)
	pag := envelope["pagination"].(map[string]interface{})
	if pag["has_more"] == nil {
		t.Errorf("response has_more should be present")
	}
}

func TestListStatements_EmptyStatements_ReturnsEmptyArray(t *testing.T) {
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
	}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	envelope := decodeBody(t, w)
	data := envelope["data"].(map[string]interface{})
	statements := data["statements"].([]interface{})
	if len(statements) != 0 {
		t.Errorf("expected empty statements array, got %d", len(statements))
	}
}

func TestListStatements_WarningsIncluded(t *testing.T) {
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
		warnings:   []string{"partial results"},
	}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	envelope := decodeBody(t, w)
	warns := envelope["warnings"].([]interface{})
	if len(warns) != 1 || warns[0] != "partial results" {
		t.Errorf("unexpected warnings: %v", warns)
	}
}

func TestListStatements_BackwardCompatibility(t *testing.T) {
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
	}
	r := stmtRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements?page_size=25", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if svc.capturedQ.Limit != 25 {
		t.Errorf("expected limit=25 from page_size, got %d", svc.capturedQ.Limit)
	}
}

func TestListStatements_AdminAccess(t *testing.T) {
	svc := &mockStatementService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
	}
	// Admin can request customer_id explicitly
	r := stmtRouterWithAuth(svc, true, "admin-1", []string{"admin"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/statements?customer_id=cust-2", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if svc.capturedCust != "cust-2" {
		t.Errorf("expected requested customer_id=cust-2, got %q", svc.capturedCust)
	}
}

