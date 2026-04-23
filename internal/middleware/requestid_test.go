package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"stellarbill-backend/internal/requestid"
)

func TestRequestIDMiddlewareBasic(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		incomingHeader string
		expectedStatus int
		expectHeader   bool
		headerPattern  string
	}{
		{
			name:           "No incoming request ID generates new one",
			incomingHeader: "",
			expectedStatus: http.StatusOK,
			expectHeader:   true,
			headerPattern:  `^[a-f0-9]{24}$`,
		},
		{
			name:           "Valid incoming request ID is replaced (untrusted source)",
			incomingHeader: "abc123def456",
			expectedStatus: http.StatusOK,
			expectHeader:   true,
			// Untrusted source: inbound ID is discarded, new 24-hex ID generated
			headerPattern: `^[a-f0-9]{24}$`,
		},
		{
			name:           "Invalid incoming request ID generates new one",
			incomingHeader: "invalid@id#with!special",
			expectedStatus: http.StatusOK,
			expectHeader:   true,
			headerPattern:  `^[a-f0-9]{24}$`,
		},
		{
			name:           "Too long incoming request ID generates new one",
			incomingHeader: "thisisaverylongrequestidthatexceedsthemaximumallowedlengthof128characterssothisshouldberejecteddefinitelyyes",
			expectedStatus: http.StatusOK,
			expectHeader:   true,
			headerPattern:  `^[a-f0-9]{24}$`,
		},
		{
			name:           "Empty incoming request ID generates new one",
			incomingHeader: "",
			expectedStatus: http.StatusOK,
			expectHeader:   true,
			headerPattern:  `^[a-f0-9]{24}$`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(RequestID())

			router.GET("/test", func(c *gin.Context) {
				rid, _ := c.Get(RequestIDKey)
				c.JSON(http.StatusOK, gin.H{"request_id": rid})
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.incomingHeader != "" {
				req.Header.Set(RequestIDHeader, tt.incomingHeader)
			}

			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectHeader {
				responseHeader := w.Header().Get(RequestIDHeader)
				assert.NotEmpty(t, responseHeader)

				matched, err := regexp.MatchString(tt.headerPattern, responseHeader)
				assert.NoError(t, err)
				assert.True(t, matched, "Response header %q does not match pattern %q", responseHeader, tt.headerPattern)
			}

			// Verify the request ID is available in context
			var response map[string]string
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.NotEmpty(t, response["request_id"])
		})
	}
}

func TestGetRequestIDFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Request ID exists in context", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(RequestIDKey, "test-request-id")

		rid, exists := c.Get(RequestIDKey)
		assert.True(t, exists)
		assert.Equal(t, "test-request-id", rid)
	})

	t.Run("Request ID does not exist in context", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())

		rid, exists := c.Get(RequestIDKey)
		assert.False(t, exists)
		assert.Nil(t, rid)
	})
}

func TestSanitizeRequestIDFunction(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected bool
	}{
		{"Valid alphanumeric ID", "abc123def456", true},
		{"Valid numeric ID", "123456789", true},
		{"Valid mixed case ID", "AbC123XyZ", true},
		{"Valid single character", "a", true},
		{"Valid with dash", "abc-123-def", true},
		{"Valid with underscore", "abc_123", true},
		{"Valid with dot", "abc.123", true},
		{"Valid 128 character ID", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"[:128], true},
		{"Empty ID", "", false},
		{"ID with spaces", "abc 123", false},
		{"ID with at-sign", "abc@123", false},
		{"ID longer than 128 characters", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + "x", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := requestid.Sanitize(tt.id)
			assert.Equal(t, tt.expected, ok)
		})
	}
}

func TestGenerateRequestIDFormat(t *testing.T) {
	t.Run("Generated ID has correct format", func(t *testing.T) {
		id := requestid.Generate()
		assert.Len(t, id, 24)

		matched, err := regexp.MatchString(`^[a-f0-9]{24}$`, id)
		assert.NoError(t, err)
		assert.True(t, matched)
	})

	t.Run("Generated IDs are unique", func(t *testing.T) {
		ids := make(map[string]bool)

		for i := 0; i < 100; i++ {
			id := requestid.Generate()
			assert.False(t, ids[id], "Generated duplicate ID: %s", id)
			ids[id] = true
		}
		assert.Len(t, ids, 100)
	})
}

func TestMiddlewareOrdering(t *testing.T) {
	gin.SetMode(gin.TestMode)

	executionOrder := []string{}

	router := gin.New()
	router.Use(RequestID())
	router.Use(func(c *gin.Context) {
		executionOrder = append(executionOrder, "second-middleware")
		c.Next()
	})

	router.GET("/test", func(c *gin.Context) {
		rid, _ := c.Get(RequestIDKey)
		executionOrder = append(executionOrder, "handler")
		c.JSON(http.StatusOK, gin.H{
			"request_id": rid,
			"order":      executionOrder,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	order := response["order"].([]interface{})
	assert.Equal(t, "second-middleware", order[0])
	assert.Equal(t, "handler", order[1])

	// Request ID should be set before second middleware runs
	assert.NotEmpty(t, response["request_id"])
}

func TestNestedMiddlewareComposition(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()

	// First level group with request ID
	v1 := router.Group("/api/v1")
	v1.Use(RequestID())

	// Second level group with additional middleware
	users := v1.Group("/users")
	users.Use(func(c *gin.Context) {
		rid, _ := c.Get(RequestIDKey)
		c.Header("X-User-Middleware-ID", rid.(string))
		c.Next()
	})

	users.GET("/:id", func(c *gin.Context) {
		rid, _ := c.Get(RequestIDKey)
		c.JSON(http.StatusOK, gin.H{
			"user_id":    c.Param("id"),
			"request_id": rid,
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/123", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Verify request ID is in response header
	responseHeader := w.Header().Get(RequestIDHeader)
	assert.NotEmpty(t, responseHeader)

	// Verify request ID is passed to nested middleware
	userMiddlewareID := w.Header().Get("X-User-Middleware-ID")
	assert.Equal(t, responseHeader, userMiddlewareID)

	// Verify request ID is available in handler
	var response map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, responseHeader, response["request_id"])
}

func TestEdgeCases(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Multiple calls to Get(RequestIDKey) return same value", func(t *testing.T) {
		router := gin.New()
		router.Use(RequestID())

		router.GET("/test", func(c *gin.Context) {
			id1, _ := c.Get(RequestIDKey)
			id2, _ := c.Get(RequestIDKey)
			id3, _ := c.Get(RequestIDKey)

			c.JSON(http.StatusOK, gin.H{
				"id1": id1,
				"id2": id2,
				"id3": id3,
			})
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var response map[string]string
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.Equal(t, response["id1"], response["id2"])
		assert.Equal(t, response["id2"], response["id3"])
		assert.NotEmpty(t, response["id1"])
	})

	t.Run("Request ID survives context changes", func(t *testing.T) {
		router := gin.New()
		router.Use(RequestID())
		router.Use(func(c *gin.Context) {
			// Simulate context modification
			c.Set("some_other_key", "some_value")
			c.Next()
		})

		router.GET("/test", func(c *gin.Context) {
			rid, _ := c.Get(RequestIDKey)
			otherValue := c.MustGet("some_other_key")

			c.JSON(http.StatusOK, gin.H{
				"request_id":  rid,
				"other_value": otherValue,
			})
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)

		assert.NotEmpty(t, response["request_id"])
		assert.Equal(t, "some_value", response["other_value"])
	})
}
