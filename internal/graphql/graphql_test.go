package graphql_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gqlpkg "stellarbill-backend/internal/graphql"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// ---- helpers ----

func buildServices() gqlpkg.Services {
	planRepo := repository.NewMockPlanRepo(
		&repository.PlanRow{ID: "p1", Name: "Starter", Amount: "1000", Currency: "USD", Interval: "monthly", Description: "starter plan"},
		&repository.PlanRow{ID: "p2", Name: "Pro", Amount: "5000", Currency: "USD", Interval: "yearly"},
	)
	subRepo := repository.NewMockSubscriptionRepo(
		&repository.SubscriptionRow{ID: "sub-1", TenantID: "t1", CustomerID: "c1", Status: "active", PlanID: "p1", Amount: "1000", Currency: "USD", Interval: "monthly"},
	)
	stmtRepo := repository.NewMockStatementRepo(
		&repository.StatementRow{ID: "st-1", SubscriptionID: "sub-1", CustomerID: "c1", PeriodStart: "2026-01-01T00:00:00Z", PeriodEnd: "2026-01-31T23:59:59Z", IssuedAt: "2026-02-01T00:00:00Z", TotalAmount: "1000", Currency: "USD", Kind: "invoice", Status: "paid"},
	)
	subSvc := service.NewSubscriptionService(subRepo, planRepo)
	stmtSvc := service.NewStatementService(subRepo, stmtRepo)
	return gqlpkg.Services{SubSvc: subSvc, StmtSvc: stmtSvc, PlanRepo: planRepo}
}

func buildHandler(t *testing.T, svc gqlpkg.Services) *gqlpkg.Handler {
	t.Helper()
	h, err := gqlpkg.NewHandler(svc)
	require.NoError(t, err)
	return h
}

func doGraphQLRequest(t *testing.T, h *gqlpkg.Handler, query string, callerID, tenantID string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{"query": query})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("callerID", callerID)
	c.Set("tenantID", tenantID)
	c.Set("roles", []string{"subscriber"})

	h.ServeHTTP(c)
	return w
}

// ---- tests: plans ----

func TestGraphQL_Plans_Success(t *testing.T) {
	h := buildHandler(t, buildServices())
	w := doGraphQLRequest(t, h, `{ plans { id name amount currency interval description } }`, "c1", "t1")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	data := resp["data"].(map[string]interface{})
	plans := data["plans"].([]interface{})
	assert.Len(t, plans, 2)

	ids := make([]string, 0, 2)
	for _, p := range plans {
		ids = append(ids, p.(map[string]interface{})["id"].(string))
	}
	assert.Contains(t, ids, "p1")
	assert.Contains(t, ids, "p2")
}

func TestGraphQL_Plans_EmptyRepo(t *testing.T) {
	svc := gqlpkg.Services{
		SubSvc:   service.NewSubscriptionService(repository.NewMockSubscriptionRepo(), repository.NewMockPlanRepo()),
		StmtSvc:  service.NewStatementService(repository.NewMockSubscriptionRepo(), repository.NewMockStatementRepo()),
		PlanRepo: repository.NewMockPlanRepo(),
	}
	h := buildHandler(t, svc)
	w := doGraphQLRequest(t, h, `{ plans { id name } }`, "c1", "t1")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	plans := resp["data"].(map[string]interface{})["plans"].([]interface{})
	assert.Len(t, plans, 0)
}

// ---- tests: subscription ----

func TestGraphQL_Subscription_Success(t *testing.T) {
	h := buildHandler(t, buildServices())
	w := doGraphQLRequest(t, h, `{ subscription(id:"sub-1") { id status interval billing_summary { amount_cents currency } plan { id name } } }`, "c1", "t1")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	sub := resp["data"].(map[string]interface{})["subscription"].(map[string]interface{})
	assert.Equal(t, "sub-1", sub["id"])
	assert.Equal(t, "active", sub["status"])
	bs := sub["billing_summary"].(map[string]interface{})
	assert.Equal(t, float64(1000), bs["amount_cents"])
	assert.Equal(t, "USD", bs["currency"])
	plan := sub["plan"].(map[string]interface{})
	assert.Equal(t, "p1", plan["id"])
}

func TestGraphQL_Subscription_TenantScopeRejected(t *testing.T) {
	h := buildHandler(t, buildServices())
	// sub-1 belongs to tenant t1 — querying with tenant t2 should fail
	w := doGraphQLRequest(t, h, `{ subscription(id:"sub-1") { id } }`, "c1", "t2")

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	errs := resp["errors"].([]interface{})
	assert.NotEmpty(t, errs)
}

func TestGraphQL_Subscription_NotFound(t *testing.T) {
	h := buildHandler(t, buildServices())
	w := doGraphQLRequest(t, h, `{ subscription(id:"no-such") { id } }`, "c1", "t1")

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestGraphQL_Subscription_ForbiddenCaller(t *testing.T) {
	h := buildHandler(t, buildServices())
	// sub-1 owned by c1, querying as c2
	w := doGraphQLRequest(t, h, `{ subscription(id:"sub-1") { id } }`, "c2", "t1")

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// ---- tests: statements ----

func TestGraphQL_Statements_Success(t *testing.T) {
	h := buildHandler(t, buildServices())
	w := doGraphQLRequest(t, h, `{ statements(customer_id:"c1") { id subscription_id period_start period_end issued_at total_amount currency kind status } }`, "c1", "t1")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	stmts := resp["data"].(map[string]interface{})["statements"].([]interface{})
	assert.Len(t, stmts, 1)
	st := stmts[0].(map[string]interface{})
	assert.Equal(t, "st-1", st["id"])
	assert.Equal(t, "invoice", st["kind"])
}

func TestGraphQL_Statements_Empty(t *testing.T) {
	h := buildHandler(t, buildServices())
	w := doGraphQLRequest(t, h, `{ statements(customer_id:"c999") { id } }`, "c999", "t1")

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	stmts := resp["data"].(map[string]interface{})["statements"].([]interface{})
	assert.Len(t, stmts, 0)
}

// ---- tests: handler validation ----

func TestGraphQL_MissingBody(t *testing.T) {
	h := buildHandler(t, buildServices())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", strings.NewReader("not-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("callerID", "c1")
	c.Set("tenantID", "t1")
	c.Set("roles", []string{})
	h.ServeHTTP(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGraphQL_EmptyQuery(t *testing.T) {
	h := buildHandler(t, buildServices())
	body, _ := json.Marshal(map[string]interface{}{"query": ""})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("callerID", "c1")
	c.Set("tenantID", "t1")
	c.Set("roles", []string{})
	h.ServeHTTP(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGraphQL_ParseError(t *testing.T) {
	h := buildHandler(t, buildServices())
	body, _ := json.Marshal(map[string]interface{}{"query": "{ unclosed {"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("callerID", "c1")
	c.Set("tenantID", "t1")
	c.Set("roles", []string{})
	h.ServeHTTP(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGraphQL_RolesFromContext_NonSlice(t *testing.T) {
	// Exercises the roles type switch when value is not []string
	h := buildHandler(t, buildServices())
	body, _ := json.Marshal(map[string]interface{}{"query": `{ plans { id } }`})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("callerID", "c1")
	c.Set("tenantID", "t1")
	// omit roles — defaults to nil
	h.ServeHTTP(c)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---- tests: depth/complexity limits ----

func TestValidateQuery_DepthExceeded(t *testing.T) {
	// Build a query 6 levels deep (limit is 5)
	deep := `{ a { b { c { d { e { f { id } } } } } } }`
	// parse via handler path
	h := buildHandler(t, buildServices())
	body, _ := json.Marshal(map[string]interface{}{"query": deep})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("callerID", "c1")
	c.Set("tenantID", "t1")
	c.Set("roles", []string{})
	h.ServeHTTP(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "depth")
}

func TestValidateQuery_ComplexityExceeded(t *testing.T) {
	// Build a query with >50 fields
	fields := strings.Repeat("f1 f2 f3 f4 f5 f6 f7 f8 f9 f10 ", 6) // 60 fields
	query := "{ " + strings.TrimSpace(fields) + " }"
	h := buildHandler(t, buildServices())
	body, _ := json.Marshal(map[string]interface{}{"query": query})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/graphql", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req
	c.Set("callerID", "c1")
	c.Set("tenantID", "t1")
	c.Set("roles", []string{})
	h.ServeHTTP(c)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "complexity")
}

func TestValidateQuery_WithinLimits(t *testing.T) {
	svc := buildServices()
	query := `{ plans { id name } }`
	// should not error
	h := buildHandler(t, svc)
	w := doGraphQLRequest(t, h, query, "c1", "t1")
	assert.NotEqual(t, http.StatusBadRequest, w.Code)
}

// ---- tests: context helpers ----

func TestWithCallerContext(t *testing.T) {
	ctx := gqlpkg.WithCallerContext(context.Background(), "user1", "tenant1", []string{"admin"})
	assert.NotNil(t, ctx)
}

// ---- tests: limits package unit tests ----

func TestMeasureQuery_Depth(t *testing.T) {
	// 3-level deep query
	err := gqlpkg.ValidateQueryString(`{ a { b { c } } }`)
	assert.NoError(t, err)
}

func TestMeasureQuery_DepthExact(t *testing.T) {
	// exactly 5 levels deep — should pass
	err := gqlpkg.ValidateQueryString(`{ a { b { c { d { e } } } } }`)
	assert.NoError(t, err)
}

func TestMeasureQuery_DepthOver(t *testing.T) {
	// 6 levels deep — should fail
	err := gqlpkg.ValidateQueryString(`{ a { b { c { d { e { f } } } } } }`)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "depth")
}

func TestMeasureQuery_ComplexityExact(t *testing.T) {
	// 50 fields — should pass
	fields := strings.Repeat("x ", 50)
	err := gqlpkg.ValidateQueryString("{ " + strings.TrimSpace(fields) + " }")
	assert.NoError(t, err)
}

func TestMeasureQuery_ComplexityOver(t *testing.T) {
	// 51 fields — should fail
	fields := strings.Repeat("x ", 51)
	err := gqlpkg.ValidateQueryString("{ " + strings.TrimSpace(fields) + " }")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "complexity")
}

func TestNewHandler_Success(t *testing.T) {
	h, err := gqlpkg.NewHandler(buildServices())
	require.NoError(t, err)
	assert.NotNil(t, h)
}

// ---- coverage gap tests ----

// errPlanRepo is a PlanRepository that always errors on List.
type errPlanRepo struct{}

func (e errPlanRepo) FindByID(_ context.Context, _ string) (*repository.PlanRow, error) {
	return nil, fmt.Errorf("db error")
}
func (e errPlanRepo) List(_ context.Context) ([]*repository.PlanRow, error) {
	return nil, fmt.Errorf("db error")
}

func TestGraphQL_Plans_RepoError(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo()
	svc := gqlpkg.Services{
		SubSvc:   service.NewSubscriptionService(subRepo, errPlanRepo{}),
		StmtSvc:  service.NewStatementService(subRepo, repository.NewMockStatementRepo()),
		PlanRepo: errPlanRepo{},
	}
	h := buildHandler(t, svc)
	w := doGraphQLRequest(t, h, `{ plans { id } }`, "c1", "t1")
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

// errStmtRepo wraps MockStatementRepo but forces ListByCustomerID to error.
type errStmtRepo struct{}

func (e errStmtRepo) FindByID(_ context.Context, _ string) (*repository.StatementRow, error) {
	return nil, fmt.Errorf("db error")
}
func (e errStmtRepo) ListByCustomerID(_ context.Context, _ string, _ repository.StatementQuery) ([]*repository.StatementRow, int, error) {
	return nil, 0, fmt.Errorf("db error")
}
func (e errStmtRepo) UpdateArchivedData(_ context.Context, _ string, _ *repository.StatementRow) error {
	return nil
}

func TestGraphQL_Statements_RepoError(t *testing.T) {
	subRepo := repository.NewMockSubscriptionRepo(
		&repository.SubscriptionRow{ID: "sub-1", TenantID: "t1", CustomerID: "c1", Status: "active", PlanID: "p1", Amount: "1000", Currency: "USD", Interval: "monthly"},
	)
	planRepo := repository.NewMockPlanRepo(&repository.PlanRow{ID: "p1", Name: "Starter", Amount: "1000", Currency: "USD", Interval: "monthly"})
	svc := gqlpkg.Services{
		SubSvc:   service.NewSubscriptionService(subRepo, planRepo),
		StmtSvc:  service.NewStatementService(subRepo, errStmtRepo{}),
		PlanRepo: planRepo,
	}
	h := buildHandler(t, svc)
	w := doGraphQLRequest(t, h, `{ statements(customer_id:"c1") { id } }`, "c1", "t1")
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestGraphQL_Subscription_NoBillingDate(t *testing.T) {
	// Covers nilIfEmpty with nil pointer (NextBillingDate is nil when NextBilling is "")
	subRepo := repository.NewMockSubscriptionRepo(
		&repository.SubscriptionRow{ID: "sub-2", TenantID: "t1", CustomerID: "c2", Status: "active", PlanID: "p1", Amount: "500", Currency: "USD", Interval: "monthly", NextBilling: ""},
	)
	planRepo := repository.NewMockPlanRepo(&repository.PlanRow{ID: "p1", Name: "Basic", Amount: "500", Currency: "USD", Interval: "monthly"})
	svc := gqlpkg.Services{
		SubSvc:   service.NewSubscriptionService(subRepo, planRepo),
		StmtSvc:  service.NewStatementService(subRepo, repository.NewMockStatementRepo()),
		PlanRepo: planRepo,
	}
	h := buildHandler(t, svc)
	w := doGraphQLRequest(t, h, `{ subscription(id:"sub-2") { id billing_summary { next_billing_date } } }`, "c2", "t1")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	sub := resp["data"].(map[string]interface{})["subscription"].(map[string]interface{})
	bs := sub["billing_summary"].(map[string]interface{})
	assert.Nil(t, bs["next_billing_date"])
}

func TestGraphQL_Subscription_WithBillingDate(t *testing.T) {
	// Covers nilIfEmpty returning the non-empty string value
	subRepo := repository.NewMockSubscriptionRepo(
		&repository.SubscriptionRow{ID: "sub-3", TenantID: "t1", CustomerID: "c3", Status: "active", PlanID: "p1", Amount: "500", Currency: "USD", Interval: "monthly", NextBilling: "2026-07-01T00:00:00Z"},
	)
	planRepo := repository.NewMockPlanRepo(&repository.PlanRow{ID: "p1", Name: "Basic", Amount: "500", Currency: "USD", Interval: "monthly"})
	svc := gqlpkg.Services{
		SubSvc:   service.NewSubscriptionService(subRepo, planRepo),
		StmtSvc:  service.NewStatementService(subRepo, repository.NewMockStatementRepo()),
		PlanRepo: planRepo,
	}
	h := buildHandler(t, svc)
	w := doGraphQLRequest(t, h, `{ subscription(id:"sub-3") { billing_summary { next_billing_date } } }`, "c3", "t1")
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	sub := resp["data"].(map[string]interface{})["subscription"].(map[string]interface{})
	bs := sub["billing_summary"].(map[string]interface{})
	assert.NotNil(t, bs["next_billing_date"])
}
