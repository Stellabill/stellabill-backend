package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/service"
)

func TestCoverage_NewHandlerWithDependencies(t *testing.T) {
	h := NewHandlerWithDependencies(nil, nil, "db", "outbox")
	if h.Database != "db" || h.Outbox != "outbox" {
		t.Fatal("dependencies not set")
	}
}

func TestCoverage_NewAdminHandler(t *testing.T) {
	h := NewAdminHandler("token")
	if h.expectedToken != "token" {
		t.Fatal("token mismatch")
	}
}

func TestCoverage_AdminHandler_PurgeCache(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAdminHandler("tok")
	r := gin.New()
	r.POST("/purge", h.PurgeCache)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/purge", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/purge", nil)
	req.Header.Set("X-Admin-Token", "tok")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCoverage_PlanGetters(t *testing.T) {
	p := Plan{ID: "p1", Name: "Plan"}
	if p.GetID() != "p1" {
		t.Fatal("GetID mismatch")
	}
	if p.GetSortValue() == "" {
		// just ensure it doesn't panic
	}
}

func TestCoverage_SubscriptionGetters(t *testing.T) {
	s := Subscription{ID: "s1", Customer: "c1"}
	if s.GetID() != "s1" {
		t.Fatal("GetID mismatch")
	}
	if s.GetSortValue() != "c1" {
		t.Fatal("GetSortValue mismatch")
	}
}

func TestCoverage_ErrorResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	RespondWithNotFoundError(c, "missing")

	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	RespondWithInternalError(c2, "boom")

	_, _, _ = MapServiceErrorToResponse(errors.New("any"))
	_, _, _ = MapServiceErrorToResponse(service.ErrNotFound)
	_, _, _ = MapServiceErrorToResponse(service.ErrDeleted)
	_, _, _ = MapServiceErrorToResponse(service.ErrForbidden)
	_, _, _ = MapServiceErrorToResponse(service.ErrBillingParse)
}

func TestCoverage_NewGetSubscriptionHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewGetSubscriptionHandler(nil)
	r := gin.New()
	r.GET("/s/:id", h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCoverage_NewGetStatementHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewGetStatementHandler(nil)
	r := gin.New()
	r.GET("/s/:id", h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/s/abc", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestCoverage_NewListStatementsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewListStatementsHandler(nil)
	r := gin.New()
	r.GET("/s", h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/s", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

type stubSubSvc struct{}

func (stubSubSvc) ListSubscriptions(c *gin.Context) ([]Subscription, error) {
	return []Subscription{{ID: "s1"}}, nil
}
func (stubSubSvc) GetSubscription(c *gin.Context, id string) (*Subscription, error) {
	return &Subscription{ID: id}, nil
}

func TestCoverage_HandlerListAndGetSubscriptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{Subscriptions: stubSubSvc{}}
	r := gin.New()
	r.GET("/subs", h.ListSubscriptions)
	r.GET("/sub/:id", h.GetSubscription)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sub/abc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Invalid cursor -> 400
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subs?cursor=!!invalid!!", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid cursor, got %d", rec.Code)
	}
}

type errSubSvc struct{}

func (errSubSvc) ListSubscriptions(c *gin.Context) ([]Subscription, error) {
	return nil, errors.New("svc failure")
}
func (errSubSvc) GetSubscription(c *gin.Context, id string) (*Subscription, error) {
	return nil, errors.New("svc failure")
}

type stubPlanSvcCov struct{}

func (stubPlanSvcCov) ListPlans(c *gin.Context) ([]Plan, error) {
	return []Plan{{ID: "p1", Name: "Basic"}}, nil
}

func TestCoverage_HandlerListPlans_BadCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{Plans: stubPlanSvcCov{}}
	r := gin.New()
	r.GET("/plans", h.ListPlans)

	// Bad cursor -> 500 (via decode error)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plans?cursor=!!bad!!", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for bad cursor, got %d", rec.Code)
	}

	// Bad limit (negative) -> defaults to 10 and succeeds
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plans?limit=-5", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for negative limit, got %d", rec.Code)
	}
}

func TestCoverage_HandlerGetters_NilDeps(t *testing.T) {
	h := &Handler{}
	if h.getDatabase() != nil {
		t.Fatal("expected nil DB for unset field")
	}
	if h.getOutboxHealther() != nil {
		t.Fatal("expected nil outbox for unset field")
	}
}

func TestCoverage_HandlerSubscriptions_Errors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{Subscriptions: errSubSvc{}}
	r := gin.New()
	r.GET("/subs", h.ListSubscriptions)
	r.GET("/sub/:id", h.GetSubscription)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/subs", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 on list error, got %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/sub/abc", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on get error, got %d", rec.Code)
	}
}
