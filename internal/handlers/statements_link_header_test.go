package handlers

import (
	"net/http"
	"net/http/httptest"
	"stellarbill-backend/internal/service"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// authedStatementsRouter builds a minimal router that sets the context keys
// getAuthContext actually reads ("caller_id" as a string, "roles" as
// []string), independent of the stmtRouter helper elsewhere in this package.
func authedStatementsRouter(svc service.StatementService, callerID string, roles []string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("caller_id", callerID)
		c.Set("roles", roles)
		c.Next()
	})
	r.GET("/api/statements", NewListStatementsHandler(svc))
	return r
}

func TestListStatements_LinkHeader_FirstOnly_NoNextOrPrev(t *testing.T) {
	svc := &mockStatementsTestService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
	}
	r := authedStatementsRouter(svc, "cust-1", []string{"customer"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/api/statements?customer_id=cust-1", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	link := w.Header().Get("Link")
	if !strings.Contains(link, `rel="first"`) {
		t.Errorf("expected rel=first link, got %q", link)
	}
	// Statements has no working keyset-pagination mechanism yet (see
	// docs/link-header-pagination.md), so next/prev must never be emitted.
	if strings.Contains(link, `rel="next"`) || strings.Contains(link, `rel="prev"`) {
		t.Errorf("expected no next/prev link for statements, got %q", link)
	}
}

func TestListStatements_LinkHeader_PreservesFilters(t *testing.T) {
	svc := &mockStatementsTestService{
		listDetail: &service.ListStatementsDetail{Statements: []*service.StatementDetail{}},
		count:      0,
	}
	r := authedStatementsRouter(svc, "cust-1", []string{"customer"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "https://api.example.com/api/statements?customer_id=cust-1&status=paid", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	link := w.Header().Get("Link")
	if !strings.Contains(link, "customer_id=cust-1") || !strings.Contains(link, "status=paid") {
		t.Errorf("expected first link to preserve filters, got %q", link)
	}
}
