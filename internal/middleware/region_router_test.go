package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegionRouter_ActiveRolePassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RegionRouterMiddleware(RegionRouterConfig{Role: RegionRoleActive}))
	r.POST("/api/v1/things", func(c *gin.Context) {
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", strings.NewReader(`{}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
}

func TestRegionRouter_PassiveReadPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RegionRouterMiddleware(RegionRouterConfig{
		Role:            RegionRolePassive,
		ActiveRegionURL: "https://active.example",
	}))
	r.GET("/api/v1/things", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"local": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/things", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "local")
}

func TestRegionRouter_PassiveRedirect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RegionRouterMiddleware(RegionRouterConfig{
		Role:            RegionRolePassive,
		ActiveRegionURL: "https://active.example",
		ForwardMode:     RegionForwardModeRedirect,
	}))
	r.POST("/api/v1/things", func(c *gin.Context) {
		t.Fatal("handler must not run on redirect")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things?x=1", strings.NewReader(`{}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://active.example/api/v1/things?x=1", w.Header().Get("Location"))
	assert.Equal(t, RegionHopValue, w.Header().Get(RegionHopHeader))
}

func TestRegionRouter_PassiveProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "/api/v1/things/1", r.URL.Path)
		assert.Equal(t, RegionHopValue, r.Header.Get(RegionHopHeader))
		assert.Equal(t, "Bearer secret-token", r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"n":1}`, string(body))
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"forwarded":true}`))
	}))
	defer upstream.Close()

	r := gin.New()
	r.Use(RegionRouterMiddleware(RegionRouterConfig{
		Role:             RegionRolePassive,
		ActiveRegionURL:  upstream.URL,
		ForwardMode:      RegionForwardModeProxy,
		ForwardAuthToken: "secret-token",
		HTTPClient:       upstream.Client(),
	}))
	r.PUT("/api/v1/things/:id", func(c *gin.Context) {
		t.Fatal("local handler must not run when proxying")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/things/1", strings.NewReader(`{"n":1}`))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusAccepted, w.Code)
	assert.Equal(t, "yes", w.Header().Get("X-Upstream"))
	assert.Equal(t, RegionHopValue, w.Header().Get(RegionHopHeader))
	assert.JSONEq(t, `{"forwarded":true}`, w.Body.String())
}

func TestRegionRouter_HopLoopRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RegionRouterMiddleware(RegionRouterConfig{
		Role:            RegionRolePassive,
		ActiveRegionURL: "https://active.example",
		ForwardMode:     RegionForwardModeProxy,
	}))
	r.POST("/api/v1/things", func(c *gin.Context) {
		t.Fatal("must not reach handler")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", strings.NewReader(`{}`))
	req.Header.Set(RegionHopHeader, RegionHopValue)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusLoopDetected, w.Code)
	assert.Contains(t, w.Body.String(), "region_hop_loop")
}

func TestRegionRouter_ActiveUnreachableReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RegionRouterMiddleware(RegionRouterConfig{
		Role:            RegionRolePassive,
		ActiveRegionURL: "http://127.0.0.1:1", // nothing listening
		ForwardMode:     RegionForwardModeProxy,
		Timeout:         200 * time.Millisecond,
		HTTPClient:      &http.Client{Timeout: 200 * time.Millisecond},
	}))
	r.DELETE("/api/v1/things/1", func(c *gin.Context) {
		t.Fatal("must not reach handler")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/things/1", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "active_region_unavailable")
}

func TestRegionRouter_MissingActiveURLReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RegionRouterMiddleware(RegionRouterConfig{
		Role:        RegionRolePassive,
		ForwardMode: RegionForwardModeProxy,
	}))
	r.POST("/api/v1/things", func(c *gin.Context) {
		t.Fatal("must not reach handler")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/things", strings.NewReader(`{}`))
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "ACTIVE_REGION_URL")
}

func TestIsWriteMethod(t *testing.T) {
	assert.True(t, isWriteMethod(http.MethodPost))
	assert.True(t, isWriteMethod(http.MethodPut))
	assert.True(t, isWriteMethod(http.MethodPatch))
	assert.True(t, isWriteMethod(http.MethodDelete))
	assert.False(t, isWriteMethod(http.MethodGet))
	assert.False(t, isWriteMethod(http.MethodHead))
	assert.False(t, isWriteMethod(http.MethodOptions))
}
