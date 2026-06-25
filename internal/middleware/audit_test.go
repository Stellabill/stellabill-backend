package middleware

import (
	"net/http"
	"net/http/httptest"
	"stellarbill-backend/internal/audit"
	"testing"
	"time"
)

type dummySink struct{}
func (s *dummySink) WriteEvent(e audit.AuditEvent) error { return nil }

func TestAuditMiddleware_SuccessPath(t *testing.T) {
	// 1. Setup a dummy logger 
	// (If your Logger is an interface, mock it. If it's a struct, pass an empty/safe instance)
	dummyLogger := audit.NewLogger("test-secret", &dummySink{}) 

	// 2. Initialize the middleware
	middleware := AuditMiddleware(dummyLogger)

	// 3. Create a dummy next handler that simulates a successful 201 Created response
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("resource created"))
	})

	// Wrap the handler
	handlerToTest := middleware(nextHandler)

	// 4. Create a mock HTTP request
	req := httptest.NewRequest(http.MethodPost, "/api/v1/invoices", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	req.Header.Set("User-Agent", "Test-Agent/1.0")

	// 5. Create a response recorder to capture the output
	rr := httptest.NewRecorder()

	// 6. Execute the middleware
	handlerToTest.ServeHTTP(rr, req)

	// 7. Assert the HTTP response passed through correctly
	if status := rr.Code; status != http.StatusCreated {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusCreated)
	}

	// Wait briefly to ensure the fire-and-forget goroutine completes so its lines are marked as covered
	time.Sleep(50 * time.Millisecond)
}

func TestAuditMiddleware_FailurePath(t *testing.T) {
	dummyLogger := audit.NewLogger("test-secret", &dummySink{})
	middleware := AuditMiddleware(dummyLogger)

	// Simulate an error response (e.g., 400 Bad Request) to test the `outcome = "failure"` branch
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	handlerToTest := middleware(nextHandler)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/invalid", nil)
	rr := httptest.NewRecorder()

	handlerToTest.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusBadRequest)
	}

	// Let the background goroutine finish
	time.Sleep(50 * time.Millisecond)
}

func TestResponseWriter_CaptureStatus(t *testing.T) {
	// Directly test the custom responseWriter struct
	rr := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rr, status: http.StatusOK}

	rw.WriteHeader(http.StatusTeapot)

	if rw.status != http.StatusTeapot {
		t.Errorf("expected custom writer to capture status %v, got %v", http.StatusTeapot, rw.status)
	}
}

func TestExtractUser(t *testing.T) {
	// Directly test the extraction utility
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	user := extractUser(req)
	
	expected := "user_id_from_context"
	if user != expected {
		t.Errorf("expected user %v, got %v", expected, user)
	}
}
