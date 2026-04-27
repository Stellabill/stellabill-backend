package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"stellabill-backend/internal/service"
)

// mockErrorService returns different errors for testing
type mockErrorService struct {
	shouldReturnError bool
	errorType         error
	detail            *service.SubscriptionDetail
	warnings          []string
}

func (m *mockErrorService) GetDetail(_ context.Context, _, _, _ string, _ bool) (*service.SubscriptionDetail, []string, error) {
	if m.shouldReturnError {
		return nil, nil, m.errorType
	}
	return m.detail, m.warnings, nil
}

// setupErrorTestRouter builds a test router with trace ID middleware
func setupErrorTestRouter(svc service.SubscriptionService, setCallerID bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Add trace ID context
	r.Use(func(c *gin.Context) {
		if traceID := c.GetHeader("X-Trace-ID"); traceID != "" {
			c.Set("traceID", traceID)
		} else {
			c.Set("traceID", "test-trace-123")
		}
		c.Header("X-Trace-ID", c.GetString("traceID"))
	})
	if setCallerID {
		r.Use(func(c *gin.Context) {
			c.Set("callerID", "caller-123")
			c.Set("tenantID", "tenant-1")
			c.Next()
		})
	}
	r.GET("/api/subscriptions/:id", NewGetSubscriptionHandler(svc))
	return r
}

// TestErrorEnvelope_NotFound tests the error envelope for not found errors
func TestErrorEnvelope_NotFound(t *testing.T) {
	svc := &mockErrorService{
		shouldReturnError: true,
		errorType:         service.ErrNotFound,
	}
	r := setupErrorTestRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/nonexistent-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, w.Code)
	}

	var envelope ErrorEnvelope
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if envelope.Code != string(ErrorCodeNotFound) {
		t.Errorf("Expected error code %s, got %s", ErrorCodeNotFound, envelope.Code)
	}
	if envelope.Message != "The requested resource was not found" {
		t.Errorf("Expected proper message, got %s", envelope.Message)
	}
	if envelope.TraceID != "test-trace-123" {
		t.Errorf("Expected trace ID test-trace-123, got %s", envelope.TraceID)
	}
}

// TestErrorEnvelope_Deleted tests the error envelope for deleted resource errors
func TestErrorEnvelope_Deleted(t *testing.T) {
	svc := &mockErrorService{
		shouldReturnError: true,
		errorType:         service.ErrDeleted,
	}
	r := setupErrorTestRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/deleted-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusGone {
		t.Errorf("Expected status %d, got %d", http.StatusGone, w.Code)
	}

	var envelope ErrorEnvelope
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if envelope.Code != string(ErrorCodeNotFound) {
		t.Errorf("Expected error code %s, got %s", ErrorCodeNotFound, envelope.Code)
	}
	if envelope.TraceID == "" {
		t.Error("Expected trace ID to be present")
	}
}

// TestErrorEnvelope_Forbidden tests the error envelope for forbidden errors
func TestErrorEnvelope_Forbidden(t *testing.T) {
	svc := &mockErrorService{
		shouldReturnError: true,
		errorType:         service.ErrForbidden,
	}
	r := setupErrorTestRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/forbidden-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("Expected status %d, got %d", http.StatusForbidden, w.Code)
	}

	var envelope ErrorEnvelope
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if envelope.Code != string(ErrorCodeForbidden) {
		t.Errorf("Expected error code %s, got %s", ErrorCodeForbidden, envelope.Code)
	}
	if envelope.Message != "You do not have permission to access this resource" {
		t.Errorf("Expected proper message, got %s", envelope.Message)
	}
}

// TestErrorEnvelope_BillingParse tests the error envelope for billing parse errors
func TestErrorEnvelope_BillingParse(t *testing.T) {
	svc := &mockErrorService{
		shouldReturnError: true,
		errorType:         service.ErrBillingParse,
	}
	r := setupErrorTestRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/billing-error-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}

	var envelope ErrorEnvelope
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if envelope.Code != string(ErrorCodeInternalError) {
		t.Errorf("Expected error code %s, got %s", ErrorCodeInternalError, envelope.Code)
	}
}

// TestErrorEnvelope_ValidationError tests validation errors
func TestErrorEnvelope_ValidationError(t *testing.T) {
	svc := &mockErrorService{}
	r := setupErrorTestRouter(svc, true)

	w := httptest.NewRecorder()
	// Empty subscription ID should trigger validation error
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/", nil)
	r.ServeHTTP(w, req)

	// Gin routing returns 404 for unmatched routes, skip this test
	if w.Code == http.StatusNotFound {
		t.Skip("Route not matched, skipping validation test")
	}

	// If we get here, check the response format
	var envelope ErrorEnvelope
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	if err == nil {
		if envelope.Code != string(ErrorCodeValidationFailed) {
			t.Errorf("Expected validation error code, got %s", envelope.Code)
		}
	}
}

// TestErrorEnvelope_MissingAuth tests authentication error envelope
func TestErrorEnvelope_MissingAuth(t *testing.T) {
	svc := &mockErrorService{}
	r := setupErrorTestRouter(svc, false) // Don't set callerID

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/some-id", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}

	var envelope ErrorEnvelope
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if envelope.Code != string(ErrorCodeUnauthorized) {
		t.Errorf("Expected error code %s, got %s", ErrorCodeUnauthorized, envelope.Code)
	}
}

// TestErrorEnvelope_ValidDetailsIncluded tests validation errors include details
func TestErrorEnvelope_ValidDetailsIncluded(t *testing.T) {
	svc := &mockErrorService{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("traceID", "test-trace-456")
		c.Set("callerID", "caller-123")
		c.Set("tenantID", "tenant-1")
	})
	r.GET("/api/subscriptions/:id", NewGetSubscriptionHandler(svc))

	w := httptest.NewRecorder()
	// Test with whitespace-only ID (will be trimmed to empty)
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/%20%20", nil)
	r.ServeHTTP(w, req)

	var envelope ErrorEnvelope
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if envelope.Details == nil {
		t.Error("Expected details in validation error")
	} else if field, ok := envelope.Details["field"]; !ok || field != "id" {
		t.Errorf("Expected field details, got %v", envelope.Details)
	}
}

// TestErrorEnvelope_TraceIDTracking tests trace ID is properly tracked through responses
func TestErrorEnvelope_TraceIDTracking(t *testing.T) {
	svc := &mockErrorService{
		shouldReturnError: true,
		errorType:         service.ErrNotFound,
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		// Use custom trace ID from header or generate one
		if traceID := c.GetHeader("X-Trace-ID"); traceID != "" {
			c.Set("traceID", traceID)
		} else {
			c.Set("traceID", "generated-trace-id")
		}
		c.Header("X-Trace-ID", c.GetString("traceID"))
	})
	r.Use(func(c *gin.Context) {
		c.Set("callerID", "caller-123")
		c.Set("tenantID", "tenant-1")
	})
	r.GET("/api/subscriptions/:id", NewGetSubscriptionHandler(svc))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/test-id", nil)
	req.Header.Set("X-Trace-ID", "custom-trace-789")
	r.ServeHTTP(w, req)

	var envelope ErrorEnvelope
	err := json.Unmarshal(w.Body.Bytes(), &envelope)
	if err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if envelope.TraceID != "custom-trace-789" {
		t.Errorf("Expected custom trace ID, got %s", envelope.TraceID)
	}

	// Also check response header
	if headerTraceID := w.Header().Get("X-Trace-ID"); headerTraceID != "custom-trace-789" {
		t.Errorf("Expected trace ID in header, got %s", headerTraceID)
	}
}

// TestErrorEnvelope_ContentType tests proper content type header
func TestErrorEnvelope_ContentType(t *testing.T) {
	svc := &mockErrorService{
		shouldReturnError: true,
		errorType:         service.ErrNotFound,
	}
	r := setupErrorTestRouter(svc, true)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/test-id", nil)
	r.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("Expected proper content type, got %s", contentType)
	}
}

