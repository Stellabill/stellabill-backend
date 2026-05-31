package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/featureflags"
)

func TestFaultInjection_Disabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag("fault_injection_enabled", false, "")

	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set(faultHeader, "status=503")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestFaultInjection_Status503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag("fault_injection_enabled", true, "")

	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set(faultHeader, "status=503")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestFaultInjection_Latency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag("fault_injection_enabled", true, "")

	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set(faultHeader, "latency=10ms")
	w := httptest.NewRecorder()
	start := time.Now()
	router.ServeHTTP(w, req)
	duration := time.Since(start)

	if duration < 10*time.Millisecond {
		t.Errorf("Expected latency at least 10ms, got %v", duration)
	}
	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestFaultInjection_NoHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag("fault_injection_enabled", true, "")

	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"message": "success"})
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestFaultInjection_CancelCtx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	featureflags.GetInstance().SetFlag("fault_injection_enabled", true, "")

	router.Use(FaultInjection())
	router.GET("/test", func(c *gin.Context) {
		select {
		case <-c.Request.Context().Done():
			c.JSON(499, gin.H{"message": "context cancelled"})
		case <-time.After(100 * time.Millisecond):
			c.JSON(200, gin.H{"message": "success"})
		}
	})

	req, _ := http.NewRequest("GET", "/test", nil)
	req.Header.Set(faultHeader, "cancel=true")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
}

func TestParseFaultHeader(t *testing.T) {
	tests := []struct {
		name     string
		header   string
		expected faultConfig
	}{
		{
			name:     "empty header",
			header:   "",
			expected: faultConfig{},
		},
		{
			name:   "latency only",
			header: "latency=500ms",
			expected: faultConfig{
				latency: 500 * time.Millisecond,
			},
		},
		{
			name:   "status only",
			header: "status=503",
			expected: faultConfig{
				status: 503,
			},
		},
		{
			name:   "prob only",
			header: "prob=0.5",
			expected: faultConfig{
				prob: 0.5,
			},
		},
		{
			name:   "cancel only",
			header: "cancel=true",
			expected: faultConfig{
				cancelCtx: true,
			},
		},
		{
			name:   "all fields",
			header: "latency=1s,status=500,prob=0.1,cancel=true",
			expected: faultConfig{
				latency:   1 * time.Second,
				status:    500,
				prob:      0.1,
				cancelCtx: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseFaultHeader(tt.header)
			if result.latency != tt.expected.latency {
				t.Errorf("Expected latency %v, got %v", tt.expected.latency, result.latency)
			}
			if result.status != tt.expected.status {
				t.Errorf("Expected status %d, got %d", tt.expected.status, result.status)
			}
			if result.prob != tt.expected.prob {
				t.Errorf("Expected prob %f, got %f", tt.expected.prob, result.prob)
			}
			if result.cancelCtx != tt.expected.cancelCtx {
				t.Errorf("Expected cancelCtx %v, got %v", tt.expected.cancelCtx, result.cancelCtx)
			}
		})
	}
}
