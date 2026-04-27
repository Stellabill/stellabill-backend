//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHealth(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/health", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]string
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("expected status=ok, got %q", body["status"])
	}
	if body["service"] != "stellabill-backend" {
		t.Errorf("expected service=stellabill-backend, got %q", body["service"])
	}
}

