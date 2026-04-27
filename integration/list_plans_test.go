//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestListPlans(t *testing.T) {
	r := buildRouter(sharedPool)
	w := do(r, http.MethodGet, "/api/plans", "")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}

	var body map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if _, ok := body["plans"]; !ok {
		t.Errorf("expected response to contain 'plans' key, got: %v", body)
	}
}

