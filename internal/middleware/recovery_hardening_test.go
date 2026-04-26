package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"stellabill-backend/internal/logger"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLogs swaps the package logger output for a buffer for the duration
// of the test. It returns the buffer and a restore function the caller must
// defer.
func captureLogs(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	var buf bytes.Buffer
	origOut := logger.Log.Out
	origFmt := logger.Log.Formatter
	origLevel := logger.Log.Level
	logger.Log.SetOutput(&buf)
	logger.Log.SetFormatter(&logrus.JSONFormatter{})
	logger.Log.SetLevel(logrus.DebugLevel)
	return &buf, func() {
		logger.Log.SetOutput(origOut)
		logger.Log.SetFormatter(origFmt)
		logger.Log.SetLevel(origLevel)
	}
}

// readLogLines parses the captured JSON-lines log buffer into a slice of
// maps. Lines that fail to parse are skipped (the test is interested in
// structured records emitted by the middleware).
func readLogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err == nil {
			out = append(out, m)
		}
	}
	return out
}

// TestRecoveryDoesNotLeakStackToClient verifies the response body never
// contains stack-frame markers, even though the middleware logs a full
// stack server-side.
func TestRecoveryDoesNotLeakStackToClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf, restore := captureLogs(t)
	defer restore()

	router := gin.New()
	router.Use(Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("boom with internals")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	body := w.Body.String()
	assert.NotContains(t, body, "goroutine ")
	assert.NotContains(t, body, "runtime/debug")
	assert.NotContains(t, body, "boom with internals",
		"raw panic message must not appear in response body")
	assert.Contains(t, body, "Internal server error")

	// Server-side log must include the (sanitized) stack and the panic
	// message — that is the whole point of the redaction split.
	lines := readLogLines(t, buf)
	require.NotEmpty(t, lines, "expected at least one log line")
	var found bool
	for _, line := range lines {
		if line["msg"] != "panic recovered" {
			continue
		}
		found = true
		assert.NotEmpty(t, line["stack"], "stack must be present in log")
		assert.NotEmpty(t, line["request_id"])
	}
	assert.True(t, found, "expected a 'panic recovered' log entry")
}

// TestRecoveryRedactsSecretsInLog verifies common credential shapes inside a
// panic value are scrubbed before reaching the structured log.
func TestRecoveryRedactsSecretsInLog(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf, restore := captureLogs(t)
	defer restore()

	router := gin.New()
	router.Use(Recovery())
	router.GET("/panic", func(c *gin.Context) {
		// A library that echoes incoming headers into an error message is a
		// realistic source of secret leakage into panics.
		panic("upstream rejected request with Authorization: Bearer sk-ABC123XYZ-supersecret-token-value")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	lines := readLogLines(t, buf)
	require.NotEmpty(t, lines)
	var saw bool
	for _, line := range lines {
		if line["msg"] != "panic recovered" {
			continue
		}
		saw = true
		panicField, _ := line["panic"].(string)
		assert.NotContains(t, panicField, "Bearer sk-ABC123XYZ")
		assert.NotContains(t, panicField, "supersecret-token-value")
		assert.Contains(t, panicField, "[REDACTED]")
	}
	assert.True(t, saw)
}

// TestRecoveryRedactsAWSKeyAndJWT covers AWS access key IDs and JWT-shaped
// strings, which are common shapes the redactor must catch.
func TestRecoveryRedactsAWSKeyAndJWT(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf, restore := captureLogs(t)
	defer restore()

	router := gin.New()
	router.Use(Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("aws AKIAIOSFODNN7EXAMPLE jwt eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1In0.signaturepart")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	for _, line := range readLogLines(t, buf) {
		if line["msg"] != "panic recovered" {
			continue
		}
		panicField, _ := line["panic"].(string)
		assert.NotContains(t, panicField, "AKIAIOSFODNN7EXAMPLE")
		assert.NotContains(t, panicField, "eyJhbGciOiJIUzI1NiJ9")
	}
}

// TestPanicInMiddlewareChain verifies a panic raised by a middleware
// installed *between* Recovery and the handler is still caught and produces
// the correct envelope.
func TestPanicInMiddlewareChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())
	router.Use(func(c *gin.Context) {
		panic("middleware blew up")
	})
	router.GET("/anything", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/anything", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, internalErrorCode, resp.Code)
	assert.NotEmpty(t, resp.Request)
	assert.Equal(t, w.Header().Get(RequestIDHeader), resp.Request,
		"X-Request-ID header and body request_id must match")
}

// TestPanicDuringResponseWrite verifies that when a handler has already
// flushed its response and then panics, Recovery does not attempt to
// rewrite the body or status, but does emit a log marker.
func TestPanicDuringResponseWrite(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf, restore := captureLogs(t)
	defer restore()

	router := gin.New()
	router.Use(Recovery())
	router.GET("/panic-after-write", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		// Simulate an error during a streaming write.
		panic("panic after response written")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic-after-write", nil)
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	})

	// The first write already set 200; the protocol does not allow us to
	// change it, and we explicitly choose not to corrupt the body.
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")

	// The log must still record the partial-response panic for triage.
	var sawPartial bool
	for _, line := range readLogLines(t, buf) {
		if line["msg"] == "panic after response started — connection will be aborted" {
			sawPartial = true
			assert.Equal(t, true, line["partial_response"])
			assert.NotEmpty(t, line["request_id"])
		}
	}
	assert.True(t, sawPartial, "expected partial-response log entry")
}

// TestRequestIDPropagatedFromHeader verifies that when RequestID middleware
// runs first, Recovery uses that id rather than minting a new one.
func TestRequestIDPropagatedFromHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())
	router.Use(RequestID())
	router.GET("/panic", func(c *gin.Context) {
		panic("noop")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set(RequestIDHeader, "abc123")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, "abc123", w.Header().Get(RequestIDHeader))
	var resp ErrorResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "abc123", resp.Request)
}

// TestPlainTextNegotiation verifies content negotiation: only an explicit
// Accept: text/plain triggers the plain-text envelope.
func TestPlainTextNegotiation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name      string
		accept    string
		wantPlain bool
	}{
		{"empty accept defaults to json", "", false},
		{"wildcard accept defaults to json", "*/*", false},
		{"json accept", "application/json", false},
		{"plain accept", "text/plain", true},
		{"plain wins when listed first", "text/plain, application/json", true},
		{"json wins when listed first", "application/json, text/plain", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(Recovery())
			router.GET("/panic", func(c *gin.Context) { panic("x") })

			req := httptest.NewRequest(http.MethodGet, "/panic", nil)
			if tc.accept != "" {
				req.Header.Set("Accept", tc.accept)
			}
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			ct := w.Header().Get("Content-Type")
			if tc.wantPlain {
				assert.Contains(t, ct, "text/plain")
				assert.Contains(t, w.Body.String(), "Request ID:")
			} else {
				assert.Contains(t, ct, "application/json")
				var resp ErrorResponse
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, internalErrorCode, resp.Code)
			}
		})
	}
}

// TestRedactSecretsUnit exercises the redactor directly so future edits to
// the pattern list have a fast unit-level regression check.
func TestRedactSecretsUnit(t *testing.T) {
	cases := map[string]string{
		"Authorization: Bearer abc.def.ghi":  "[REDACTED]",
		"password=hunter2":                   "[REDACTED]",
		"api_key=ABC123":                     "[REDACTED]",
		"token: 1234567890":                  "[REDACTED]",
		"AKIAIOSFODNN7EXAMPLE":               "[REDACTED]",
		"eyJhbGciOiJIUzI1NiJ9.payload.sig":   "[REDACTED]",
	}
	for input, want := range cases {
		got := redactSecrets(input)
		assert.Equal(t, want, got, "input=%q", input)
	}
}

// drainBody is a small helper used in some assertion paths.
var _ = io.Discard
var _ = bytes.NewBuffer
