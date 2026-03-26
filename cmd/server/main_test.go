package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNewRouterRegistersMiddlewareAndRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := newRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if res.Header().Get("X-Request-ID") == "" {
		t.Fatal("expected request id header")
	}
	if res.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("expected CORS header")
	}
}
