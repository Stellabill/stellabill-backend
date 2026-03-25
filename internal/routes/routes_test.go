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
