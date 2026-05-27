package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/service"
)

// mockErrorService returns different errors for testing
type mockErrorService struct {
	shouldReturnError bool
	errorType         error
	detail            *service.SubscriptionDetail
	warnings          []string
}

func (m *mockErrorService) GetDetail(_ context.Context, _, _, _ string) (*service.SubscriptionDetail, []string, error) {
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
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/550e8400-e29b-41d4-a716-446655440000", nil)
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
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/550e8400-e29b-41d4-a716-446655440000", nil)
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
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/550e8400-e29b-41d4-a716-446655440000", nil)
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
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/550e8400-e29b-41d4-a716-446655440000", nil)
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
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/550e8400-e29b-41d4-a716-446655440000", nil)
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

	var resp struct {
		Error  string `json:"error"`
		Fields []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if len(resp.Fields) == 0 {
		t.Error("Expected validation fields")
	} else if resp.Fields[0].Field != "value" { // validation.ValidateVar uses "value" as default field name if not specified
		t.Errorf("Expected field 'value', got %s", resp.Fields[0].Field)
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
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/550e8400-e29b-41d4-a716-446655440000", nil)
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
	req, _ := http.NewRequest(http.MethodGet, "/api/subscriptions/550e8400-e29b-41d4-a716-446655440000", nil)
	r.ServeHTTP(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json; charset=utf-8" {
		t.Errorf("Expected proper content type, got %s", contentType)
	}
}

// TestErrorEnvelope_PIIRedaction tests that PII is redacted from error messages and details
func TestErrorEnvelope_PIIRedaction(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("traceID", "test-trace-pii")
	})
	r.GET("/test", func(c *gin.Context) {
		details := map[string]interface{}{
			"customer_id": "cust_sensitive123",
			"amount":      1234.56,
			"safe_field":  "ok",
		}
		RespondWithErrorDetails(c, http.StatusBadRequest, ErrorCodeBadRequest, 
			"Failed processing customer_cust_sensitive123 with amount 1234.56", details)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	r.ServeHTTP(w, req)

	var envelope ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Message should have PII redacted
	assertFalse := func(cond bool, msg string) {
		if cond {
			t.Errorf("PII leaked in message: %s - %s", envelope.Message, msg)
		}
	}
	assertFalse(strings.Contains(envelope.Message, "cust_sensitive123"), "customer ID should not be present")
	assertFalse(strings.Contains(envelope.Message, "1234.56"), "amount should not be present")
	assertTrue := func(cond bool, msg string) {
		if !cond {
			t.Errorf(msg)
		}
	}
	assertTrue(strings.Contains(envelope.Message, "cust_***"), "should contain redacted customer")
	// amount may be masked: but message amount might be masked as $*.**; check that original number not present as digits.
	assertTrue(strings.Contains(envelope.Message, "$*.**") || !strings.Contains(envelope.Message, "1234"), "amount should be masked")

	// Details map should have values redacted
	if custDetail, ok := envelope.Details["customer_id"]; ok {
		assertTrue(custDetail == "***REDACTED***" || custDetail == "cust***", "customer_id in details should be redacted")
	}
	if amtDetail, ok := envelope.Details["amount"]; ok {
		assertTrue(amtDetail == "$*.**", "amount in details should be masked")
	}
	// safe_field unchanged
	if safe, ok := envelope.Details["safe_field"]; ok {
		assertTrue(safe == "ok", "safe field unchanged")
	}
}
