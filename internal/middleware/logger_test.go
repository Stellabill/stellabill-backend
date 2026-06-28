package middleware

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/logger"
)

func TestLoggerNeverLeaksSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []struct {
		name           string
		method         string
		path           string
		headers        map[string]string
		body           string
		bannedSubstrs  []string
	}{
		{
			name:   "Authorization header redacted",
			method: "GET",
			path:   "/api/health",
			headers: map[string]string{
				"Authorization": "Bearer secret-token-123",
			},
			bannedSubstrs: []string{"secret-token-123"},
		},
		{
			name:   "X-Admin-Token header redacted",
			method: "POST",
			path:   "/api/admin/purge",
			headers: map[string]string{
				"X-Admin-Token": "admin-secret-456",
			},
			bannedSubstrs: []string{"admin-secret-456"},
		},
		{
			name:           "JWT in query string redacted",
			method:         "GET",
			path:           "/api/health?jwt=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.test.signature",
			headers:        map[string]string{},
			bannedSubstrs:  []string{"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"},
		},
		{
			name:   "Password in JSON body redacted",
			method: "POST",
			path:   "/api/health",
			headers: map[string]string{
				"Content-Type": "application/json",
			},
			body:           `{"username": "test", "password": "mysecretpass"}`,
			bannedSubstrs:  []string{"mysecretpass"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger.Log.SetOutput(&buf)
			logger.Log.SetFormatter(logger.NewLogSchemaFormatter(false))

			r := gin.New()
			r.Use(RequestLogger())
			r.GET("/api/health", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})
			r.POST("/api/health", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})
			r.POST("/api/admin/purge", func(c *gin.Context) {
				c.String(http.StatusOK, "ok")
			})

			req := httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			logOutput := buf.String()
			t.Logf("Log output: %s", logOutput)

			for _, substr := range tc.bannedSubstrs {
				if bytes.Contains([]byte(logOutput), []byte(substr)) {
					t.Errorf("banned substring '%s' found in log output: %s", substr, logOutput)
				}
			}

			var logEntry map[string]interface{}
			if err := json.Unmarshal(buf.Bytes(), &logEntry); err == nil {
				required := []string{"time", "level", "msg"}
				for _, key := range required {
					if _, ok := logEntry[key]; !ok {
						t.Errorf("required key '%s' missing from log entry", key)
					}
				}
			}
		})
	}
}
