package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"stellarbill-backend/internal/outbox"
)

func setupAttemptsRouter(repo outbox.AttemptRepository) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/webhooks/:id/attempts", func(c *gin.Context) {
		// Simulate auth middleware injecting tenantID.
		c.Set("tenantID", "tenant-abc")
		NewWebhookAttemptsHandler(repo)(c)
	})
	return r
}

func setupAttemptsRouterWithTenant(repo outbox.AttemptRepository, tenantID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/api/v1/webhooks/:id/attempts", func(c *gin.Context) {
		if tenantID != "" {
			c.Set("tenantID", tenantID)
		}
		NewWebhookAttemptsHandler(repo)(c)
	})
	return r
}

// --- Happy path: returns attempts for the correct tenant ---

func TestWebhookAttemptsHandler_HappyPath(t *testing.T) {
	repo := outbox.NewMemAttemptRepository()
	eventID := uuid.New()
	code := 200
	latency := 42
	body := "ok"

	_ = repo.SaveAttempt(&outbox.Attempt{
		EventID:       eventID,
		TenantID:      "tenant-abc",
		AttemptNumber: 1,
		ResponseCode:  &code,
		LatencyMs:     &latency,
		ResponseBody:  &body,
		AttemptedAt:   time.Now().UTC(),
	})

	r := setupAttemptsRouter(repo)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/webhooks/"+eventID.String()+"/attempts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	items, ok := resp["attempts"].([]interface{})
	if !ok || len(items) != 1 {
		t.Fatalf("expected 1 attempt, got %v", resp["attempts"])
	}
}

// --- No attempts yet: returns empty array, not null ---

func TestWebhookAttemptsHandler_NoAttempts(t *testing.T) {
	repo := outbox.NewMemAttemptRepository()
	eventID := uuid.New()

	r := setupAttemptsRouter(repo)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/webhooks/"+eventID.String()+"/attempts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items, ok := resp["attempts"].([]interface{})
	if !ok {
		t.Fatalf("expected attempts array, got %T", resp["attempts"])
	}
	if len(items) != 0 {
		t.Fatalf("expected 0 attempts, got %d", len(items))
	}
}

// --- Cross-tenant: tenant-B cannot see tenant-A attempts ---

func TestWebhookAttemptsHandler_CrossTenantIsolation(t *testing.T) {
	repo := outbox.NewMemAttemptRepository()
	eventID := uuid.New()
	code := 200

	_ = repo.SaveAttempt(&outbox.Attempt{
		EventID:       eventID,
		TenantID:      "tenant-A",
		AttemptNumber: 1,
		ResponseCode:  &code,
		AttemptedAt:   time.Now().UTC(),
	})

	// Caller is tenant-B.
	r := setupAttemptsRouterWithTenant(repo, "tenant-B")
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/webhooks/"+eventID.String()+"/attempts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	items := resp["attempts"].([]interface{})
	if len(items) != 0 {
		t.Fatalf("cross-tenant leak: expected 0 attempts for tenant-B, got %d", len(items))
	}
}

// --- Invalid UUID returns 400 ---

func TestWebhookAttemptsHandler_InvalidID(t *testing.T) {
	repo := outbox.NewMemAttemptRepository()
	r := setupAttemptsRouter(repo)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/webhooks/not-a-uuid/attempts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// --- Missing tenant context returns 401 ---

func TestWebhookAttemptsHandler_MissingTenant(t *testing.T) {
	repo := outbox.NewMemAttemptRepository()
	r := setupAttemptsRouterWithTenant(repo, "") // no tenantID set

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/webhooks/"+uuid.New().String()+"/attempts", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
