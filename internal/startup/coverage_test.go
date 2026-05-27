package startup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
)

func TestCoverage_DiagnosticsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := config.Config{}
	h := NewDiagnosticsHandler(cfg, nil, nil)

	r := gin.New()
	r.GET("/diag", h.Handle)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/diag", nil)
	r.ServeHTTP(rec, req)

	// Hit cached path too.
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req)
}

func TestCoverage_FormatResults_AllStatuses(t *testing.T) {
	results := []CheckResult{
		{Name: "a", Status: StatusPass, Message: "ok", DurationMs: 1},
		{Name: "b", Status: StatusFail, Message: "bad", DurationMs: 2},
		{Name: "c", Status: StatusWarn, Message: "warn", DurationMs: 3},
	}
	_ = FormatResults(results)
	_ = HasFailures(results)
	_ = OverallStatus(results)
	_ = OverallStatus([]CheckResult{{Status: StatusPass}})
	_ = OverallStatus([]CheckResult{{Status: StatusWarn}})
}

type stubPinger struct{ err error }

func (s stubPinger) PingContext(ctx context.Context) error { return s.err }

func TestCoverage_RunChecks(t *testing.T) {
	results := RunChecks(config.Config{}, stubPinger{}, func(ctx context.Context) (int, int, error) {
		return 0, 0, nil
	})
	if len(results) == 0 {
		t.Fatal("expected results")
	}

	// nil migration func path
	_ = RunChecks(config.Config{}, stubPinger{}, nil)

	// failing migration func
	_ = RunChecks(config.Config{}, stubPinger{}, func(ctx context.Context) (int, int, error) {
		return 0, 0, errors.New("oh no")
	})

	// pending migrations
	_ = RunChecks(config.Config{}, stubPinger{}, func(ctx context.Context) (int, int, error) {
		return 1, 5, nil
	})
}
