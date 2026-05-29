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

// ---------------------------------------------------------------------------
// mock StatementService
// ---------------------------------------------------------------------------

type mockStatementService struct {
	listResult *service.ListStatementsDetail
	listTotal  int
	listErr    error

	getResult *service.StatementDetail
	getErr    error
}

func (m *mockStatementService) ListByCustomer(
	_ context.Context,
	callerID string,
	roles []string,
	customerID string,
	_ repository.StatementQuery,
) (*service.ListStatementsDetail, int, []string, error) {
	return m.listResult, m.listTotal, nil, m.listErr
}

func (m *mockStatementService) GetDetail(
	_ context.Context,
	callerID string,
	roles []string,
	statementID string,
) (*service.StatementDetail, []string, error) {
	return m.getResult, nil, m.getErr
}

// ---------------------------------------------------------------------------
// router helpers
// ---------------------------------------------------------------------------

func withAuth(method, path, callerID string, roles []string, h gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", callerID)
		c.Set("roles", roles)
		c.Next()
	})
	r.Handle(method, path, h)
	return r
}

func noAuth(method, path string, h gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Handle(method, path, h)
	return r
}

func do(r *gin.Engine, method, url string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, url, nil)
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// NewListStatementsHandler
// ---------------------------------------------------------------------------

func TestListStatements_NilSvc_ReturnsEmpty200(t *testing.T) {
	h := NewListStatementsHandler(nil)
	r := noAuth(http.MethodGet, "/api/v1/statements", h)
	w := do(r, http.MethodGet, "/api/v1/statements")
	if w.Code != http.StatusOK {
		t.Fatalf("nil svc: expected 200, got %d", w.Code)
	}
}

func TestListStatements_NoAuth_Returns401(t *testing.T) {
	svc := &mockStatementService{}
	h := NewListStatementsHandler(svc)
	r := noAuth(http.MethodGet, "/api/v1/statements", h)
	w := do(r, http.MethodGet, "/api/v1/statements")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestListStatements_MissingCustomerID_Returns400(t *testing.T) {
	svc := &mockStatementService{}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListStatements_HappyPath(t *testing.T) {
	svc := &mockStatementService{
		listResult: &service.ListStatementsDetail{
			Statements: []*service.StatementDetail{
				{ID: "stmt-1", Kind: "invoice", Status: "paid"},
			},
		},
		listTotal: 1,
	}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body struct {
		Statements []service.StatementDetail `json:"statements"`
		Total      int                       `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(body.Statements) != 1 {
		t.Errorf("expected 1 statement, got %d", len(body.Statements))
	}
	if body.Total != 1 {
		t.Errorf("expected total 1, got %d", body.Total)
	}
}

func TestListStatements_EmptyResultSet(t *testing.T) {
	svc := &mockStatementService{
		listResult: &service.ListStatementsDetail{Statements: nil},
		listTotal:  0,
	}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&body)
	stmts, ok := body["statements"].([]interface{})
	if !ok {
		t.Fatal("statements field must be an array, not null")
	}
	if len(stmts) != 0 {
		t.Errorf("expected empty array, got %d items", len(stmts))
	}
}

func TestListStatements_ForbiddenFromService_Returns403(t *testing.T) {
	svc := &mockStatementService{listErr: service.ErrForbidden}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "attacker", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestListStatements_ServiceError_Returns500(t *testing.T) {
	svc := &mockStatementService{listErr: errors.New("db offline")}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestListStatements_InvalidStartAfter_Returns400(t *testing.T) {
	svc := &mockStatementService{}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1&start_after=not-a-date")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListStatements_InvalidEndBefore_Returns400(t *testing.T) {
	svc := &mockStatementService{}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1&end_before=not-a-date")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListStatements_InvalidLimit_Returns400(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc"} {
		svc := &mockStatementService{}
		h := NewListStatementsHandler(svc)
		r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
		w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1&limit="+bad)
		if w.Code != http.StatusBadRequest {
			t.Errorf("limit=%q: expected 400, got %d", bad, w.Code)
		}
	}
}

func TestListStatements_LimitCappedAtMax(t *testing.T) {
	svc := &mockStatementService{
		listResult: &service.ListStatementsDetail{},
		listTotal:  0,
	}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1&limit=9999")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for over-limit (capped), got %d", w.Code)
	}
}

func TestListStatements_InvalidOrder_Returns400(t *testing.T) {
	svc := &mockStatementService{}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1&order=sideways")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestListStatements_UnknownKind_PassedThrough(t *testing.T) {
	svc := &mockStatementService{
		listResult: &service.ListStatementsDetail{},
		listTotal:  0,
	}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-1&kind=unknown_kind_xyz")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestListStatements_ValidDatesAndOrder(t *testing.T) {
	svc := &mockStatementService{
		listResult: &service.ListStatementsDetail{},
		listTotal:  0,
	}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet,
		"/api/v1/statements?customer_id=cust-1&start_after=2024-01-01T00:00:00Z&end_before=2025-01-01T00:00:00Z&order=asc&limit=5")
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestListStatements_AdminCanListAnyCustomer(t *testing.T) {
	svc := &mockStatementService{
		listResult: &service.ListStatementsDetail{
			Statements: []*service.StatementDetail{{ID: "stmt-x"}},
		},
		listTotal: 1,
	}
	h := NewListStatementsHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements", "admin-user", []string{"admin"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements?customer_id=cust-99")
	if w.Code != http.StatusOK {
		t.Fatalf("admin: expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// NewGetStatementHandler
// ---------------------------------------------------------------------------

func TestGetStatement_NilSvc_Returns200WithID(t *testing.T) {
	h := NewGetStatementHandler(nil)
	r := noAuth(http.MethodGet, "/api/v1/statements/:id", h)
	w := do(r, http.MethodGet, "/api/v1/statements/stmt-abc")
	if w.Code != http.StatusOK {
		t.Fatalf("nil svc: expected 200, got %d", w.Code)
	}
}

func TestGetStatement_NoAuth_Returns401(t *testing.T) {
	svc := &mockStatementService{}
	h := NewGetStatementHandler(svc)
	r := noAuth(http.MethodGet, "/api/v1/statements/:id", h)
	w := do(r, http.MethodGet, "/api/v1/statements/stmt-1")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestGetStatement_HappyPath(t *testing.T) {
	svc := &mockStatementService{
		getResult: &service.StatementDetail{
			ID:     "stmt-1",
			Kind:   "invoice",
			Status: "paid",
		},
	}
	h := NewGetStatementHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements/:id", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements/stmt-1")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var body service.StatementDetail
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.ID != "stmt-1" {
		t.Errorf("expected id stmt-1, got %q", body.ID)
	}
}

func TestGetStatement_NotFound_Returns404(t *testing.T) {
	svc := &mockStatementService{getErr: service.ErrNotFound}
	h := NewGetStatementHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements/:id", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements/does-not-exist")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetStatement_SoftDeleted_Returns404(t *testing.T) {
	svc := &mockStatementService{getErr: service.ErrDeleted}
	h := NewGetStatementHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements/:id", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements/stmt-del")
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for deleted, got %d", w.Code)
	}
}

func TestGetStatement_WrongCustomer_Returns403(t *testing.T) {
	svc := &mockStatementService{getErr: service.ErrForbidden}
	h := NewGetStatementHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements/:id", "attacker", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements/stmt-owned-by-other")
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestGetStatement_ServiceError_Returns500(t *testing.T) {
	svc := &mockStatementService{getErr: errors.New("db offline")}
	h := NewGetStatementHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements/:id", "cust-1", []string{"customer"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements/stmt-1")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestGetStatement_AdminCanFetchAny(t *testing.T) {
	svc := &mockStatementService{
		getResult: &service.StatementDetail{ID: "stmt-other-cust"},
	}
	h := NewGetStatementHandler(svc)
	r := withAuth(http.MethodGet, "/api/v1/statements/:id", "admin-user", []string{"admin"}, h)
	w := do(r, http.MethodGet, "/api/v1/statements/stmt-other-cust")
	if w.Code != http.StatusOK {
		t.Fatalf("admin: expected 200, got %d", w.Code)
	}
}