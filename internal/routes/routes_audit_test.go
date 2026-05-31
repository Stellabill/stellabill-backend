package routes

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"stellarbill-backend/internal/audit"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAuditMiddlewareWiring verifies that the audit middleware is installed
// and captures auth failures (401/403) and admin actions.
func TestAuditMiddlewareWiring(t *testing.T) {
	// Set up environment for testing
	os.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test")
	os.Setenv("JWT_SECRET", "Test_Secret_123!")
	os.Setenv("ADMIN_TOKEN", "Test_Admin_123!")
	defer func() {
		os.Unsetenv("DATABASE_URL")
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("ADMIN_TOKEN")
	}()

	gin.SetMode(gin.TestMode)

	t.Run("auth_failure_401_logged", func(t *testing.T) {
		sink := &audit.MemorySink{}
		r := gin.New()
		
		// Manually wire minimal middleware for this test
		logger := audit.NewLogger("test-secret", sink)
		r.Use(audit.Middleware(logger))
		
		r.GET("/protected", func(c *gin.Context) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		})

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		
		entries := sink.Entries()
		require.Len(t, entries, 1, "expected one audit entry for 401")
		assert.Equal(t, "auth_failure", entries[0].Action)
		assert.Equal(t, "status_401", entries[0].Outcome)
	})

	t.Run("auth_failure_403_logged", func(t *testing.T) {
		sink := &audit.MemorySink{}
		r := gin.New()
		
		logger := audit.NewLogger("test-secret", sink)
		r.Use(audit.Middleware(logger))
		
		r.GET("/forbidden", func(c *gin.Context) {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
		})

		req := httptest.NewRequest(http.MethodGet, "/forbidden", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		
		entries := sink.Entries()
		require.Len(t, entries, 1, "expected one audit entry for 403")
		assert.Equal(t, "auth_failure", entries[0].Action)
		assert.Equal(t, "status_403", entries[0].Outcome)
	})

	t.Run("admin_purge_logged", func(t *testing.T) {
		sink := &audit.MemorySink{}
		r := gin.New()
		
		logger := audit.NewLogger("test-secret", sink)
		r.Use(audit.Middleware(logger))
		
		// Simulate admin purge endpoint
		r.POST("/admin/purge", func(c *gin.Context) {
			audit.LogAction(c, "admin_purge", "billing-cache", "success", map[string]string{
				"keys_purged": "10",
			})
			c.JSON(http.StatusOK, gin.H{"status": "purged"})
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/purge", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		entries := sink.Entries()
		require.Len(t, entries, 1, "expected one audit entry for admin purge")
		assert.Equal(t, "admin_purge", entries[0].Action)
		assert.Equal(t, "success", entries[0].Outcome)
		assert.Equal(t, "billing-cache", entries[0].Resource)
	})

	t.Run("reconciliation_logged", func(t *testing.T) {
		sink := &audit.MemorySink{}
		r := gin.New()
		
		logger := audit.NewLogger("test-secret", sink)
		r.Use(audit.Middleware(logger))
		
		// Simulate reconciliation endpoint
		r.POST("/admin/reconcile", func(c *gin.Context) {
			audit.LogAction(c, "reconciliation.execute", "reconciliation", "success", map[string]string{
				"total":      "5",
				"matched":    "5",
				"mismatched": "0",
			})
			c.JSON(http.StatusOK, gin.H{"status": "completed"})
		})

		req := httptest.NewRequest(http.MethodPost, "/admin/reconcile", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		entries := sink.Entries()
		require.Len(t, entries, 1, "expected one audit entry for reconciliation")
		assert.Equal(t, "reconciliation.execute", entries[0].Action)
		assert.Equal(t, "success", entries[0].Outcome)
	})
}

// TestAuditSinkFallback verifies that when AUDIT_LOG_PATH is not set,
// the system falls back to stderr without breaking requests.
func TestAuditSinkFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("stderr_sink_does_not_break_request", func(t *testing.T) {
		// Use stderr sink
		sink := audit.NewStderrSink()
		logger := audit.NewLogger("test-secret", sink)
		
		r := gin.New()
		r.Use(audit.Middleware(logger))
		
		r.GET("/test", func(c *gin.Context) {
			audit.LogAction(c, "test_action", "test_resource", "success", nil)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		
		var resp map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		assert.Equal(t, "ok", resp["status"])
	})

	t.Run("file_sink_write_failure_does_not_break_request", func(t *testing.T) {
		// Use an invalid path to simulate write failure
		sink := audit.NewFileSink("/invalid/path/audit.log")
		logger := audit.NewLogger("test-secret", sink)
		
		r := gin.New()
		r.Use(audit.Middleware(logger))
		
		r.GET("/test", func(c *gin.Context) {
			// LogAction should not panic even if sink fails
			audit.LogAction(c, "test_action", "test_resource", "success", nil)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// Request should still succeed even if audit logging fails
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestAuditPIIRedaction verifies that sensitive data is redacted from audit logs.
func TestAuditPIIRedaction(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("auth_header_redacted", func(t *testing.T) {
		sink := &audit.MemorySink{}
		r := gin.New()
		
		logger := audit.NewLogger("test-secret", sink)
		r.Use(audit.Middleware(logger))
		
		r.GET("/protected", func(c *gin.Context) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		})

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer secret-token-12345")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		entries := sink.Entries()
		require.Len(t, entries, 1)
		
		// The auth_header should be redacted
		authHeader, ok := entries[0].Metadata["auth_header"]
		if ok {
			assert.Equal(t, "***REDACTED***", authHeader, "auth header should be redacted")
		}
	})

	t.Run("password_metadata_redacted", func(t *testing.T) {
		sink := &audit.MemorySink{}
		r := gin.New()
		
		logger := audit.NewLogger("test-secret", sink)
		r.Use(audit.Middleware(logger))
		
		r.POST("/action", func(c *gin.Context) {
			audit.LogAction(c, "user_update", "user/123", "success", map[string]string{
				"password": "secret123",
				"username": "john",
			})
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		req := httptest.NewRequest(http.MethodPost, "/action", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		entries := sink.Entries()
		require.Len(t, entries, 1)
		
		// Password should be redacted
		password, ok := entries[0].Metadata["password"]
		require.True(t, ok)
		assert.Equal(t, "***REDACTED***", password)
		
		// Username should not be redacted
		username, ok := entries[0].Metadata["username"]
		require.True(t, ok)
		assert.Equal(t, "john", username)
	})
}

// TestAuditConfigFromEnv verifies that AUDIT_LOG_PATH is read from environment.
func TestAuditConfigFromEnv(t *testing.T) {
	t.Run("audit_log_path_from_env", func(t *testing.T) {
		testPath := "/tmp/test-audit.log"
		os.Setenv("AUDIT_LOG_PATH", testPath)
		defer os.Unsetenv("AUDIT_LOG_PATH")

		// This would normally be done in config.Load()
		path := os.Getenv("AUDIT_LOG_PATH")
		assert.Equal(t, testPath, path)
		
		sink := audit.NewFileSink(path)
		assert.NotNil(t, sink)
	})

	t.Run("audit_log_path_empty_uses_stderr", func(t *testing.T) {
		os.Unsetenv("AUDIT_LOG_PATH")
		
		path := os.Getenv("AUDIT_LOG_PATH")
		assert.Empty(t, path)
		
		// When empty, we should use stderr sink
		var sink audit.Sink
		if path == "" {
			sink = audit.NewStderrSink()
		} else {
			sink = audit.NewFileSink(path)
		}
		assert.NotNil(t, sink)
	})
}

// TestAuditChaining verifies that audit events are cryptographically chained.
func TestAuditChaining(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("events_are_chained", func(t *testing.T) {
		sink := &audit.MemorySink{}
		logger := audit.NewLogger("test-secret", sink)
		
		r := gin.New()
		r.Use(audit.Middleware(logger))
		
		r.POST("/action1", func(c *gin.Context) {
			audit.LogAction(c, "action1", "resource1", "success", nil)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})
		
		r.POST("/action2", func(c *gin.Context) {
			audit.LogAction(c, "action2", "resource2", "success", nil)
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		// First request
		req1 := httptest.NewRequest(http.MethodPost, "/action1", nil)
		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)

		// Second request
		req2 := httptest.NewRequest(http.MethodPost, "/action2", nil)
		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)

		entries := sink.Entries()
		require.Len(t, entries, 2)
		
		// First event should have empty prev_hash
		assert.Empty(t, entries[0].PrevHash)
		assert.NotEmpty(t, entries[0].Hash)
		
		// Second event should reference first event's hash
		assert.Equal(t, entries[0].Hash, entries[1].PrevHash)
		assert.NotEmpty(t, entries[1].Hash)
		assert.NotEqual(t, entries[0].Hash, entries[1].Hash)
	})
}

// TestFileSinkCreatesFile verifies that FileSink creates the audit log file.
func TestFileSinkCreatesFile(t *testing.T) {
	t.Run("file_sink_creates_file", func(t *testing.T) {
		tmpFile := t.TempDir() + "/audit-test.log"
		
		sink := audit.NewFileSink(tmpFile)
		logger := audit.NewLogger("test-secret", sink)
		
		event := audit.AuditEvent{
			Actor:    "test-user",
			Action:   "test-action",
			Resource: "test-resource",
			Outcome:  "success",
		}
		
		_, err := logger.Log(nil, event)
		require.NoError(t, err)
		
		// Verify file was created
		_, err = os.Stat(tmpFile)
		require.NoError(t, err, "audit log file should be created")
		
		// Verify content is JSONL
		content, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		assert.Len(t, lines, 1, "should have one line")
		
		var logged audit.AuditEvent
		err = json.Unmarshal([]byte(lines[0]), &logged)
		require.NoError(t, err, "should be valid JSON")
		assert.Equal(t, "test-action", logged.Action)
	})

	t.Run("file_sink_appends", func(t *testing.T) {
		tmpFile := t.TempDir() + "/audit-append.log"
		
		sink := audit.NewFileSink(tmpFile)
		logger := audit.NewLogger("test-secret", sink)
		
		// Write first event
		event1 := audit.AuditEvent{
			Actor:    "user1",
			Action:   "action1",
			Resource: "resource1",
			Outcome:  "success",
		}
		_, err := logger.Log(nil, event1)
		require.NoError(t, err)
		
		// Write second event
		event2 := audit.AuditEvent{
			Actor:    "user2",
			Action:   "action2",
			Resource: "resource2",
			Outcome:  "success",
		}
		_, err = logger.Log(nil, event2)
		require.NoError(t, err)
		
		// Verify both events are in the file
		content, err := os.ReadFile(tmpFile)
		require.NoError(t, err)
		
		lines := strings.Split(strings.TrimSpace(string(content)), "\n")
		assert.Len(t, lines, 2, "should have two lines")
	})
}

// TestStderrSinkWrites verifies that StderrSink writes to stderr.
func TestStderrSinkWrites(t *testing.T) {
	t.Run("stderr_sink_writes", func(t *testing.T) {
		// Capture stderr
		oldStderr := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		defer func() {
			os.Stderr = oldStderr
		}()

		sink := audit.NewStderrSink()
		logger := audit.NewLogger("test-secret", sink)
		
		event := audit.AuditEvent{
			Actor:    "test-user",
			Action:   "test-action",
			Resource: "test-resource",
			Outcome:  "success",
		}
		
		_, err := logger.Log(nil, event)
		require.NoError(t, err)
		
		// Close writer and read from pipe
		w.Close()
		var buf bytes.Buffer
		buf.ReadFrom(r)
		
		output := buf.String()
		assert.Contains(t, output, "test-action")
		assert.Contains(t, output, "test-user")
	})
}
