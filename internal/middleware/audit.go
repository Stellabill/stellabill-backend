package middleware

import (
	"bytes"
	"context"
	"net/http"
	"stellabill/internal/audit" // Adjust based on your module name
	"time"
)

// AuditMiddleware captures request details and commits them to the audit log.
func AuditMiddleware(log *audit.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// 1. Extract identity (Assuming you've already authenticated the user)
			// If not authenticated yet, it will fallback to "system/anonymous"
			actor := extractUser(r) 
			ctx := audit.WithActor(r.Context(), actor)

			// 2. Wrap the response writer to capture the status code
			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			
			// 3. Process the request
			next.ServeHTTP(wrapped, r.WithContext(ctx))

			// 4. Fire-and-forget the log entry so we don't slow down the response
			go func() {
				outcome := "success"
				if wrapped.status >= 400 {
					outcome = "failure"
				}

				_, _ = log.Log(context.Background(), audit.AuditEvent{
					Actor:    actor,
					Action:   r.Method,
					Resource: r.URL.Path,
					Outcome:  outcome,
					Metadata: map[string]interface{}{
						"latency_ms": time.Since(start).Milliseconds(),
						"status":     wrapped.status,
						"ip":         r.RemoteAddr,
						"user_agent": r.UserAgent(),
					},
				})
			}()
		})
	}
}

// Simple wrapper to capture HTTP status codes
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func extractUser(r *http.Request) string {
	// Logic to pull user ID from JWT or Session
	// This is a placeholder for your auth logic
	return "user_id_from_context" 
}
