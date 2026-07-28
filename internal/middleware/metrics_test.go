package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestCostAccountingMiddleware_RecordsHeaderAndMetric(t *testing.T) {
	gin.SetMode(gin.TestMode)
	TenantCostUnitsTotal.Reset()

	router := gin.New()
	router.Use(CostAccountingMiddleware())
	router.GET("/billing", func(c *gin.Context) {
		c.Set("tenantID", "tenant-123")

		acc := AccumulatorFromGinContext(c)
		acc.AddDBRowsRead(3)
		acc.AddExternalCall(2)
		acc.AddEgressBytes(2048)

		_, _ = c.Writer.WriteString("ok")
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/billing", nil)
	router.ServeHTTP(w, req)

	if got := w.Header().Get("X-Cost-Units"); got != "25" {
		t.Fatalf("expected X-Cost-Units header to be 25, got %q", got)
	}

	if got := testutil.ToFloat64(TenantCostUnitsTotal.WithLabelValues("tenant-123")); got != 1 {
		t.Fatalf("expected tenant cost metric to be recorded once, got %v", got)
	}
}

func TestCostAccountingMiddleware_CapsTenantLabel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	TenantCostUnitsTotal.Reset()

	router := gin.New()
	router.Use(CostAccountingMiddleware())
	router.GET("/tenant", func(c *gin.Context) {
		c.Set("tenantID", strings.Repeat("a", 70))
		acc := AccumulatorFromGinContext(c)
		acc.AddDBRowsRead(1)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tenant", nil)
	router.ServeHTTP(w, req)

	capped := strings.Repeat("a", 64)
	if got := testutil.ToFloat64(TenantCostUnitsTotal.WithLabelValues(capped)); got != 1 {
		t.Fatalf("expected metric for capped tenant label %q to be recorded once, got %v", capped, got)
	}

	if got := testutil.ToFloat64(TenantCostUnitsTotal.WithLabelValues(strings.Repeat("a", 70))); got != 0 {
		t.Fatalf("expected no metric for uncapped long tenant label, got %v", got)
	}
}
