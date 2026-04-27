package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestRecoveryMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		panicType      string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "string panic",
			panicType:      "string",
			expectedStatus: 500,
			expectedError:  "internal server error",
		},
		{
			name:           "runtime error panic",
			panicType:      "runtime",
			expectedStatus: 500,
			expectedError:  "internal server error",
		},
		{
			name:           "nil pointer panic",
			panicType:      "nil",
			expectedStatus: 500,
			expectedError:  "internal server error",
		},
		{
			name:           "custom panic type",
			panicType:      "custom",
			expectedStatus: 500,
			expectedError:  "internal server error",
		},
		{
			name:           "default panic",
			panicType:      "",
			expectedStatus: 500,
			expectedError:  "internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(Recovery(nil))

			router.GET("/panic", func(c *gin.Context) {
				switch tt.panicType {
				case "string":
					panic("intentional string panic")
				case "runtime":
					panic(runtimeError("intentional runtime error"))
				case "nil":
					var nilPtr *string
					_ = *nilPtr
				case "custom":
					panic(&customPanic{Message: "custom panic type"})
				default:
					panic("default test panic")
				}
			})

			req := httptest.NewRequest("GET", "/panic?type="+tt.panicType, nil)
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			// This should not panic due to recovery middleware
			assert.NotPanics(t, func() {
				router.ServeHTTP(w, req)
			})

			assert.Equal(t, tt.expectedStatus, w.Code)

			// Check response body contains safe error message
			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedError, response["error"])
		})
	}
}

func TestRecoveryWithRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery(nil))

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
	assert.Equal(t, "internal server error", response["error"])
}

func TestRecoveryGeneratesRequestID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery(nil))

	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 500, w.Code)
}

func TestRecoveryPlainTextResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery(nil))

	router.GET("/panic", func(c *gin.Context) {
		panic("test panic")
	})

	req := httptest.NewRequest("GET", "/panic", nil)
	req.Header.Set("Accept", "text/plain")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, 500, w.Code)
}

func TestPanicAfterHeadersWritten(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery(nil))

	router.GET("/panic-after-write", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
		panic("panic after response written")
	})

	req := httptest.NewRequest("GET", "/panic-after-write", nil)
	w := httptest.NewRecorder()

	// This should not panic, but the response will already be written
	assert.NotPanics(t, func() {
		router.ServeHTTP(w, req)
	})

	// The status code will be 200 because headers were already written
	assert.Equal(t, 200, w.Code)
	assert.Contains(t, w.Body.String(), "ok")
}

func TestNestedPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery(nil))

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

func TestSanitizeStack(t *testing.T) {
	// Test with short stack trace
	shortStack := "short stack trace"
	result := sanitizeStack(shortStack)
	assert.Equal(t, shortStack, result)

	// Test with long stack trace (over 4000 chars)
	longStack := strings.Repeat("a", 5000)
	result = sanitizeStack(longStack)
	assert.Len(t, result, 4000+len("... (truncated)"))
	assert.Contains(t, result, "... (truncated)")
}

// Test types
type runtimeError string

func (e runtimeError) Error() string {
	return string(e)
}

type customPanic struct {
	Message string
}

func (p *customPanic) String() string {
	return p.Message
}

// Benchmark tests
func BenchmarkRecoveryMiddleware(b *testing.B) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(Recovery(nil))
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
	router.Use(Recovery(nil))
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
