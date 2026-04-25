package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/routes"
)

func setupIntegrationTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	routes.Register(router)
	return router
}

func TestHealthEndpoint_NoFeatureFlags(t *testing.T) {
	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["status"] != "ok" {
		t.Error("Health endpoint should work without feature flags")
	}
}

func TestSubscriptionsEndpoint_FlagEnabled(t *testing.T) {
	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/subscriptions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestSubscriptionsEndpoint_FlagDisabled(t *testing.T) {
	os.Setenv("FF_SUBSCRIPTIONS_ENABLED", "false")
	defer os.Unsetenv("FF_SUBSCRIPTIONS_ENABLED")

	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/subscriptions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["error"] != "feature_unavailable" {
		t.Error("Expected feature_unavailable error")
	}
}

func TestPlansEndpoint_FlagEnabled(t *testing.T) {
	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}
}

func TestPlansEndpoint_FlagDisabled(t *testing.T) {
	os.Setenv("FF_PLANS_ENABLED", "false")
	defer os.Unsetenv("FF_PLANS_ENABLED")

	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/plans", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestNewBillingFlowEndpoint_FlagDisabled(t *testing.T) {
	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/billing/new-flow", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestNewBillingFlowEndpoint_FlagEnabled(t *testing.T) {
	os.Setenv("FF_NEW_BILLING_FLOW", "true")
	defer os.Unsetenv("FF_NEW_BILLING_FLOW")

	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/billing/new-flow", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["message"] != "New billing flow is enabled" {
		t.Error("Expected new billing flow message")
	}
}

func TestAdvancedAnalyticsEndpoint_FlagsDisabled(t *testing.T) {
	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/analytics/advanced", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestAdvancedAnalyticsEndpoint_OneFlagEnabled(t *testing.T) {
	os.Setenv("FF_ADVANCED_ANALYTICS", "true")
	defer os.Unsetenv("FF_ADVANCED_ANALYTICS")

	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/analytics/advanced", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 503 {
		t.Errorf("Expected status 503, got %d", w.Code)
	}
}

func TestAdvancedAnalyticsEndpoint_AllFlagsEnabled(t *testing.T) {
	os.Setenv("FF_ADVANCED_ANALYTICS", "true")
	os.Setenv("FF_SUBSCRIPTIONS_ENABLED", "true")
	defer func() {
		os.Unsetenv("FF_ADVANCED_ANALYTICS")
		os.Unsetenv("FF_SUBSCRIPTIONS_ENABLED")
	}()

	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/analytics/advanced", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &response)
	if response["message"] != "Advanced analytics available" {
		t.Error("Expected advanced analytics message")
	}
}

func TestJSONFeatureFlags(t *testing.T) {
	flagsJSON := `{"subscriptions_enabled": false, "plans_enabled": false}`
	os.Setenv("FEATURE_FLAGS", flagsJSON)
	defer os.Unsetenv("FEATURE_FLAGS")

	router := setupIntegrationTestRouter()

	req1, _ := http.NewRequest("GET", "/api/subscriptions", nil)
	w1 := httptest.NewRecorder()
	router.ServeHTTP(w1, req1)

	if w1.Code != 503 {
		t.Errorf("Expected subscriptions to be disabled, got status %d", w1.Code)
	}

	req2, _ := http.NewRequest("GET", "/api/plans", nil)
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)

	if w2.Code != 503 {
		t.Errorf("Expected plans to be disabled, got status %d", w2.Code)
	}
}

func TestFeatureFlagPriority(t *testing.T) {
	os.Setenv("FEATURE_FLAGS", `{"new_billing_flow": false}`)
	os.Setenv("FF_NEW_BILLING_FLOW", "true")
	defer func() {
		os.Unsetenv("FEATURE_FLAGS")
		os.Unsetenv("FF_NEW_BILLING_FLOW")
	}()

	router := setupIntegrationTestRouter()

	req, _ := http.NewRequest("GET", "/api/billing/new-flow", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("Expected status 200 (FF_ prefix should override JSON), got %d", w.Code)
	}
}

func TestConfigIntegration(t *testing.T) {
	os.Setenv("FF_DEFAULT_ENABLED", "true")
	os.Setenv("FF_LOG_DISABLED", "false")
	defer func() {
		os.Unsetenv("FF_DEFAULT_ENABLED")
		os.Unsetenv("FF_LOG_DISABLED")
	}()

	cfg := config.Load()

	if !cfg.FeatureFlags.DefaultEnabled {
		t.Error("Default enabled should be true")
	}

	if cfg.FeatureFlags.LogDisabled {
		t.Error("Log disabled should be false")
	}
}
