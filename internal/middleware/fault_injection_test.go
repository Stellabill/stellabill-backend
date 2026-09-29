package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"stellarbill-backend/internal/featureflags"

	"github.com/gin-gonic/gin"
)

func TestFaultInjection_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag(featureflags.FaultInjectionEnabledFlag, false, "")

	router.Use(func(c *gin.Context) {
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(faultHeader, "status=503")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestFaultInjection_AdminCanInjectStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag(featureflags.FaultInjectionEnabledFlag, true, "")

	router.Use(func(c *gin.Context) {
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(faultHeader, "status=503")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}
}

func TestFaultInjection_NonAdminCannotInject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag(featureflags.FaultInjectionEnabledFlag, true, "")

	router.Use(func(c *gin.Context) {
		c.Set("roles", []string{"customer"})
		c.Next()
	})
	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(faultHeader, "status=503")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected non-admin request to pass through, got %d", w.Code)
	}
}

func TestFaultInjection_InvalidHeaderReturns400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag(featureflags.FaultInjectionEnabledFlag, true, "")

	router.Use(func(c *gin.Context) {
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(faultHeader, "status=oops,prob=0.5")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestFaultInjection_LatencyBoundedAndProbZeroNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag(featureflags.FaultInjectionEnabledFlag, true, "")

	router.Use(func(c *gin.Context) {
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(faultHeader, "latency=30s,prob=0")
	w := httptest.NewRecorder()
	start := time.Now()
	router.ServeHTTP(w, req)
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("prob=0 should bypass fault injection; elapsed=%v", time.Since(start))
	}
	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
}

func TestFaultInjection_CancelCtx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag(featureflags.FaultInjectionEnabledFlag, true, "")

	router.Use(func(c *gin.Context) {
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		select {
		case <-c.Request.Context().Done():
			c.JSON(499, gin.H{"message": "context cancelled"})
		case <-time.After(100 * time.Millisecond):
			c.JSON(http.StatusOK, gin.H{"message": "success"})
		}
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(faultHeader, "cancel=true")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 499 {
		t.Fatalf("expected cancelled context to yield 499, got %d", w.Code)
	}
}

func TestParseFaultHeader(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		want    FaultSpec
		wantErr bool
	}{
		{name: "empty header", header: "", want: FaultSpec{}},
		{name: "latency only", header: "latency=500ms", want: FaultSpec{Latency: 500 * time.Millisecond}},
		{name: "status only", header: "status=503", want: FaultSpec{Status: 503}},
		{name: "prob only", header: "prob=0.5", want: FaultSpec{Prob: 0.5}},
		{name: "cancel only", header: "cancel=true", want: FaultSpec{Cancel: true}},
		{name: "all fields", header: "latency=1s,status=500,prob=0.1,cancel=true", want: FaultSpec{Latency: 1 * time.Second, Status: 500, Prob: 0.1, Cancel: true}},
		{name: "duplicate keys use last value", header: "status=503,status=500", want: FaultSpec{Status: 500}},
		{name: "out of bounds latency capped", header: "latency=30s", want: FaultSpec{Latency: maxFaultLatency}},
		{name: "invalid key", header: "oops=1", wantErr: true},
		{name: "invalid status", header: "status=900", wantErr: true},
		{name: "invalid prob", header: "prob=2", wantErr: true},
		{name: "missing value", header: "status=", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFaultHeader(tt.header)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.header)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.header, err)
			}
			if got.Latency != tt.want.Latency {
				t.Fatalf("latency mismatch: got=%v want=%v", got.Latency, tt.want.Latency)
			}
			if got.Status != tt.want.Status {
				t.Fatalf("status mismatch: got=%d want=%d", got.Status, tt.want.Status)
			}
			if got.Prob != tt.want.Prob {
				t.Fatalf("prob mismatch: got=%v want=%v", got.Prob, tt.want.Prob)
			}
			if got.Cancel != tt.want.Cancel {
				t.Fatalf("cancel mismatch: got=%v want=%v", got.Cancel, tt.want.Cancel)
			}
		})
	}
}

func TestFaultInjection_UsesAdminRolesFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag(featureflags.FaultInjectionEnabledFlag, true, "")

	router.Use(func(c *gin.Context) {
		c.Set("roles", []string{"admin"})
		c.Next()
	})
	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", strings.NewReader(""))
	req.Header.Set(faultHeader, "status=503")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected injected error, got %d", w.Code)
	}
}
