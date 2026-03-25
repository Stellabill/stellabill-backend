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

func TestAdminDefaultToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &audit.MemorySink{}
	logger := audit.NewLogger("secret", sink)
	r := gin.New()
	r.Use(audit.Middleware(logger))

	handler := NewAdminHandler("")
	r.POST("/api/admin/purge", handler.PurgeCache)

	req, _ := http.NewRequest("POST", "/api/admin/purge", nil)
	req.Header.Set("X-Admin-Token", "change-me-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with default token, got %d", rec.Code)
	}
}
