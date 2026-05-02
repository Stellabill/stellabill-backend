package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRecoveryMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		panicValue     any
		expectedStatus int
	}{
		{"string panic", "intentional string panic", 500},
		{"runtime error panic", testRuntimeErr("intentional runtime error"), 500},
		{"custom panic type", &testCustomPanic{Msg: "custom panic type"}, 500},
		{"default panic", "default test panic", 500},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(Recovery())

			router.GET("/panic", func(c *gin.Context) {
				panic(tt.panicValue)
			})

			req := httptest.NewRequest("GET", "/panic", nil)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			assert.NotPanics(t, func() {
				router.ServeHTTP(w, req)
			})

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, internalErrorMessage, response["error"])
		})
	}
}

func TestRecoveryWithRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())

	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	req.Header.Set("X-Request-ID", "test-request-123")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 500, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, internalErrorMessage, response["error"])
}

func TestRecoveryGeneratesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())

	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 500, w.Code)

	var resp ErrorResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Request, "request_id must be generated when none provided")
}

func TestRecoveryPlainTextResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())

	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	req.Header.Set("Accept", "text/plain")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 500, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/plain")
	assert.Contains(t, w.Body.String(), "Request ID:")
}

func TestPanicAfterHeadersWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())

	router.GET("/panic-after-write", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
		panic("panic after response written")
	})

	req := httptest.NewRequest("GET", "/panic-after-write", nil)
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	})

	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestNestedPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())

	router.GET("/nested-panic", func(c *gin.Context) {
		func() {
			defer func() {
				if err := recover(); err != nil {
					panic("nested panic during recovery")
				}
			}()
			panic("initial panic")
		}()
	})

	req := httptest.NewRequest("GET", "/nested-panic", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	assert.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	})

	assert.Equal(t, 500, w.Code)
}

func TestSanitizeStackTruncation(t *testing.T) {
	short := "short stack trace"
	assert.Equal(t, short, sanitizeStack(short))

	long := strings.Repeat("a", 5000)
	result := sanitizeStack(long)
	assert.Len(t, result, maxStackBytes+len("... (truncated)"))
	assert.Contains(t, result, "... (truncated)")
}

// Test-local types to avoid collisions with other _test.go files.
type testRuntimeErr string

func (e testRuntimeErr) Error() string { return string(e) }

type testCustomPanic struct{ Msg string }

func (p *testCustomPanic) String() string { return p.Msg }

func BenchmarkRecoveryMiddleware(b *testing.B) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())
	router.GET("/test", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest("GET", "/test", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func BenchmarkRecoveryWithPanic(b *testing.B) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery())
	router.GET("/panic", func(c *gin.Context) {
		panic("benchmark panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}
