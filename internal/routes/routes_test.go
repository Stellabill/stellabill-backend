package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegister_HealthAndCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Register(engine)

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/health", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin: got %q want %q", got, "*")
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("payload.status: got %v want %q", payload["status"], "ok")
	}
	if payload["service"] != "stellarbill-backend" {
		t.Fatalf("payload.service: got %v want %q", payload["service"], "stellarbill-backend")
	}
}

func TestRegister_CORSPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Register(engine)

	req := httptest.NewRequest(http.MethodOptions, "http://localhost:8080/api/health", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusNoContent)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatalf("expected Access-Control-Allow-Methods to be set")
	}
}

func TestRegister_GetSubscriptionShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Register(engine)

	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/subscriptions/sub_123", nil)
	req.Header.Set("X-Role", "admin")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status: got %d want %d", rec.Code, http.StatusOK)
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode json: %v", err)
	}
	if payload["id"] != "sub_123" {
		t.Fatalf("payload.id: got %v want %q", payload["id"], "sub_123")
	}
	if _, ok := payload["plan_id"]; !ok {
		t.Fatalf("expected payload.plan_id to be present")
	}
	if _, ok := payload["customer"]; !ok {
		t.Fatalf("expected payload.customer to be present")
	}
}

func TestRegister_RBACMatrix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Register(engine)

	tests := []struct {
		name   string
		method string
		path   string
		role   string
		want   int
	}{
		{name: "health public", method: http.MethodGet, path: "/api/health", want: http.StatusOK},
		{name: "plans requires auth", method: http.MethodGet, path: "/api/plans", want: http.StatusUnauthorized},
		{name: "plans allows customer", method: http.MethodGet, path: "/api/plans", role: "customer", want: http.StatusOK},
		{name: "subscriptions allows merchant", method: http.MethodGet, path: "/api/subscriptions", role: "merchant", want: http.StatusOK},
		{name: "subscriptions denies customer", method: http.MethodGet, path: "/api/subscriptions", role: "customer", want: http.StatusForbidden},
		{name: "subscription detail allows admin", method: http.MethodGet, path: "/api/subscriptions/sub_123", role: "admin", want: http.StatusOK},
		{name: "admin diagnostics denies merchant", method: http.MethodGet, path: "/api/admin/diagnostics", role: "merchant", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "http://localhost:8080"+tt.path, nil)
			if tt.role != "" {
				req.Header.Set("X-Role", tt.role)
			}
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if rec.Code != tt.want {
				t.Fatalf("status: got %d want %d", rec.Code, tt.want)
			}
		})
	}
}
