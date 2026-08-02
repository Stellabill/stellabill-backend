package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestGetBuildInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.GET("/internal/buildinfo", GetBuildInfo)

	req, _ := http.NewRequest(http.MethodGet, "/internal/buildinfo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp BuildInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.GoVersion == "" {
		t.Error("expected GoVersion to be populated")
	}
	if resp.OS == "" {
		t.Error("expected OS to be populated")
	}
	if resp.Architecture == "" {
		t.Error("expected Architecture to be populated")
	}
}
