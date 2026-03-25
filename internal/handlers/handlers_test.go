package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, w
}

// --- Health ---

func TestHealth(t *testing.T) {
	c, w := newContext(http.MethodGet, "/api/health")
	Health(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Errorf("status field: got %q, want %q", body["status"], "ok")
	}
	if body["service"] != "stellarbill-backend" {
		t.Errorf("service field: got %q", body["service"])
	}
}

// --- Plans ---

func TestListPlans(t *testing.T) {
	c, w := newContext(http.MethodGet, "/api/plans")
	ListPlans(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	plans, ok := body["plans"]
	if !ok {
		t.Fatal("response missing 'plans' key")
	}
	if plans == nil {
		t.Fatal("plans is nil")
	}
}

// --- Subscriptions ---

func TestListSubscriptions(t *testing.T) {
	c, w := newContext(http.MethodGet, "/api/subscriptions")
	ListSubscriptions(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, ok := body["subscriptions"]; !ok {
		t.Fatal("response missing 'subscriptions' key")
	}
}

func TestGetSubscription_WithID(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscriptions/sub_123", nil)
	c.Params = gin.Params{{Key: "id", Value: "sub_123"}}

	GetSubscription(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != "sub_123" {
		t.Errorf("id: got %v, want %q", body["id"], "sub_123")
	}
}

func TestGetSubscription_EmptyID(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/subscriptions/", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}

	GetSubscription(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}
