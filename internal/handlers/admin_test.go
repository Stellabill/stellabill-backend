package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
)

func TestAdminPurgeSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &audit.MemorySink{}
	logger := audit.NewLogger("secret", sink)
	r := gin.New()
	r.Use(audit.Middleware(logger))

	handler := NewAdminHandler("token")
	r.POST("/api/admin/purge", handler.PurgeCache)

	req, _ := http.NewRequest("POST", "/api/admin/purge?target=cache&attempt=2", nil)
	req.Header.Set("X-Admin-Token", "token")
	req.Header.Set("X-Admin-User", "root")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	entry := sink.Entries()[0]
	if entry.Outcome != "success" || entry.Target != "cache" {
		t.Fatalf("unexpected audit entry: %+v", entry)
	}
	if entry.Metadata["attempt"] != "2" {
		t.Fatalf("attempt metadata missing: %+v", entry.Metadata)
	}
}

func TestAdminPurgePartialAndRetry(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &audit.MemorySink{}
	logger := audit.NewLogger("secret", sink)
	r := gin.New()
	r.Use(audit.Middleware(logger))

	handler := NewAdminHandler("token")
	r.POST("/api/admin/purge", handler.PurgeCache)

	req, _ := http.NewRequest("POST", "/api/admin/purge?partial=1&attempt=3", nil)
	req.Header.Set("X-Admin-Token", "token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rec.Code)
	}
	entry := sink.Entries()[0]
	if entry.Outcome != "partial" {
		t.Fatalf("expected partial outcome, got %+v", entry)
	}
	if entry.Metadata["attempt"] != "3" {
		t.Fatalf("expected attempt metadata, got %+v", entry.Metadata)
	}
}

func TestAdminPurgeDenied(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &audit.MemorySink{}
	logger := audit.NewLogger("secret", sink)
	r := gin.New()
	r.Use(audit.Middleware(logger))

	handler := NewAdminHandler("token")
	r.POST("/api/admin/purge", handler.PurgeCache)

	req, _ := http.NewRequest("POST", "/api/admin/purge", nil)
	req.Header.Set("X-Admin-Token", "wrong")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
	entry := sink.Entries()[0]
	if entry.Outcome != "denied" || entry.Action != "admin_purge" {
		t.Fatalf("expected denied audit entry, got %+v", entry)
	}
}

func TestAdminSecurityScenarios(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		configToken    string
		tokenHeader    string
		hasHeader      bool
		expectedStatus int
	}{
		{
			name:           "Valid token",
			configToken:    "secure-token",
			tokenHeader:    "secure-token",
			hasHeader:      true,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Invalid token (different length)",
			configToken:    "secure-token",
			tokenHeader:    "wrongtoken",
			hasHeader:      true,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Invalid token (same length)",
			configToken:    "secure-token",
			tokenHeader:    "wrong-tokens", // same length as "secure-token" (12 chars)
			hasHeader:      true,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Empty token header",
			configToken:    "secure-token",
			tokenHeader:    "",
			hasHeader:      true,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Missing token header entirely",
			configToken:    "secure-token",
			tokenHeader:    "",
			hasHeader:      false,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Empty configured token with empty header",
			configToken:    "",
			tokenHeader:    "",
			hasHeader:      true,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Empty configured token with change-me-admin-token",
			configToken:    "",
			tokenHeader:    "change-me-admin-token",
			hasHeader:      true,
			expectedStatus: http.StatusUnauthorized,
		},
		{
			name:           "Empty configured token with arbitrary header",
			configToken:    "",
			tokenHeader:    "some-token",
			hasHeader:      true,
			expectedStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sink := &audit.MemorySink{}
			logger := audit.NewLogger("secret", sink)
			r := gin.New()
			r.Use(audit.Middleware(logger))

			handler := NewAdminHandler(tc.configToken)
			r.POST("/api/admin/purge", handler.PurgeCache)

			req, _ := http.NewRequest("POST", "/api/admin/purge", nil)
			if tc.hasHeader {
				req.Header.Set("X-Admin-Token", tc.tokenHeader)
			}
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != tc.expectedStatus {
				t.Errorf("expected status %d, got %d", tc.expectedStatus, rec.Code)
			}

			// For unauthorized requests, make sure the response format is uniform
			if tc.expectedStatus == http.StatusUnauthorized {
				expectedBody := `{"error":"invalid admin token"}`
				if rec.Body.String() != expectedBody {
					t.Errorf("expected response body %q, got %q", expectedBody, rec.Body.String())
				}
				if len(sink.Entries()) != 1 {
					t.Fatalf("expected 1 audit entry, got %d", len(sink.Entries()))
				}
				entry := sink.Entries()[0]
				if entry.Outcome != "denied" || entry.Metadata["reason"] != "invalid_token" {
					t.Errorf("unexpected audit entry attributes: %+v", entry)
				}
			}
		})
	}
}
