package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"stellarbill-backend/internal/middleware"
)

func TestIdempotencyMiddleware_BypassNonMutating(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	store := middleware.NewInMemoryIdempotencyStore()
	r.Use(middleware.Idempotency(store))

	handlerCalled := 0
	r.GET("/test", func(c *gin.Context) {
		handlerCalled++
		c.String(http.StatusOK, "ok")
	})

	// GET request with Idempotency-Key should bypass and execute
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Idempotency-Key", "key-1")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, handlerCalled)
	assert.Empty(t, w.Header().Get("Idempotency-Replayed"))
}

func TestIdempotencyMiddleware_MissingKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	store := middleware.NewInMemoryIdempotencyStore()
	r.Use(middleware.Idempotency(store))

	r.POST("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Idempotency-Key header is required", resp["error"])
}

func TestIdempotencyMiddleware_KeyTooLong(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	store := middleware.NewInMemoryIdempotencyStore()
	r.Use(middleware.Idempotency(store))

	r.POST("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// Key longer than 255 characters
	longKey := ""
	for i := 0; i < 260; i++ {
		longKey += "a"
	}

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("{}"))
	req.Header.Set("Idempotency-Key", longKey)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Idempotency-Key is too long", resp["error"])
}

func TestIdempotencyMiddleware_SuccessAndReplay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	store := middleware.NewInMemoryIdempotencyStore()
	r.Use(middleware.Idempotency(store))

	handlerCount := 0
	r.POST("/test", func(c *gin.Context) {
		handlerCount++
		var body map[string]interface{}
		err := c.BindJSON(&body)
		require.NoError(t, err)
		c.JSON(http.StatusCreated, gin.H{"handler_called": handlerCount, "input": body["input"]})
	})

	// First Request
	req1 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"input":"hello"}`))
	req1.Header.Set("Idempotency-Key", "my-key")
	w1 := httptest.NewRecorder()

	r.ServeHTTP(w1, req1)

	assert.Equal(t, http.StatusCreated, w1.Code)
	assert.Empty(t, w1.Header().Get("Idempotency-Replayed"))
	var resp1 map[string]interface{}
	err := json.Unmarshal(w1.Body.Bytes(), &resp1)
	require.NoError(t, err)
	assert.Equal(t, float64(1), resp1["handler_called"])
	assert.Equal(t, "hello", resp1["input"])

	// Replay Request
	req2 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"input":"hello"}`))
	req2.Header.Set("Idempotency-Key", "my-key")
	w2 := httptest.NewRecorder()

	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusCreated, w2.Code)
	assert.Equal(t, "true", w2.Header().Get("Idempotency-Replayed"))
	var resp2 map[string]interface{}
	err = json.Unmarshal(w2.Body.Bytes(), &resp2)
	require.NoError(t, err)
	assert.Equal(t, float64(1), resp2["handler_called"]) // Stays 1, handler was not re-executed
	assert.Equal(t, "hello", resp2["input"])
}

func TestIdempotencyMiddleware_MismatchRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	store := middleware.NewInMemoryIdempotencyStore()
	r.Use(middleware.Idempotency(store))

	r.POST("/test", func(c *gin.Context) {
		c.String(http.StatusOK, "ok")
	})

	// First Request
	req1 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"data":"one"}`))
	req1.Header.Set("Idempotency-Key", "same-key")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusOK, w1.Code)

	// Second Request with different body
	req2 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString(`{"data":"two"}`))
	req2.Header.Set("Idempotency-Key", "same-key")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusUnprocessableEntity, w2.Code)
	var resp2 map[string]string
	err := json.Unmarshal(w2.Body.Bytes(), &resp2)
	require.NoError(t, err)
	assert.Equal(t, "Idempotency-Key reused with a different request", resp2["error"])
}

func TestIdempotencyMiddleware_FailedResponseNotCached(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	store := middleware.NewInMemoryIdempotencyStore()
	r.Use(middleware.Idempotency(store))

	handlerCount := 0
	r.POST("/test", func(c *gin.Context) {
		handlerCount++
		if handlerCount == 1 {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "temporary failure"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
	})

	// First attempt returns error
	req1 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("{}"))
	req1.Header.Set("Idempotency-Key", "fail-key")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)
	assert.Equal(t, http.StatusInternalServerError, w1.Code)

	// Second attempt succeeds and handler executes again
	req2 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("{}"))
	req2.Header.Set("Idempotency-Key", "fail-key")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusOK, w2.Code)
	var resp2 map[string]interface{}
	err := json.Unmarshal(w2.Body.Bytes(), &resp2)
	require.NoError(t, err)
	assert.True(t, resp2["success"].(bool))
}

func TestIdempotencyMiddleware_ConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	store := middleware.NewInMemoryIdempotencyStore()
	r.Use(middleware.Idempotency(store))

	handlerStarted := make(chan struct{})
	handlerResume := make(chan struct{})

	r.POST("/test", func(c *gin.Context) {
		close(handlerStarted)
		<-handlerResume
		c.String(http.StatusOK, "slow-ok")
	})

	// Fire first request asynchronously
	var wg sync.WaitGroup
	wg.Add(1)
	var w1 *httptest.ResponseRecorder
	go func() {
		defer wg.Done()
		req1 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("{}"))
		req1.Header.Set("Idempotency-Key", "concurrent-key")
		w1 = httptest.NewRecorder()
		r.ServeHTTP(w1, req1)
	}()

	// Wait for handler to start execution
	<-handlerStarted

	// Fire second request synchronously (expecting 409 Conflict)
	req2 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("{}"))
	req2.Header.Set("Idempotency-Key", "concurrent-key")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	assert.Equal(t, http.StatusConflict, w2.Code)
	var resp2 map[string]string
	err := json.Unmarshal(w2.Body.Bytes(), &resp2)
	require.NoError(t, err)
	assert.Equal(t, "Concurrent request in progress", resp2["error"])

	// Resume and complete first request
	close(handlerResume)
	wg.Wait()

	assert.Equal(t, http.StatusOK, w1.Code)
	assert.Equal(t, "slow-ok", w1.Body.String())
}

func TestIdempotencyMiddleware_TenantAndCallerIsolation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	store := middleware.NewInMemoryIdempotencyStore()
	r.Use(middleware.Idempotency(store))

	handlerCount := 0
	r.POST("/test", func(c *gin.Context) {
		handlerCount++
		c.JSON(http.StatusOK, gin.H{"count": handlerCount})
	})

	// User 1 on Tenant 1
	req1 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("{}"))
	req1.Header.Set("Idempotency-Key", "iso-key")
	w1 := httptest.NewRecorder()

	r.ServeHTTP(w1, func() *httptest.ResponseRecorder {
		// Mock authentication context
		ctx, _ := gin.CreateTestContext(w1)
		ctx.Request = req1
		ctx.Set("tenantID", "tenant-1")
		ctx.Set("callerID", "caller-1")
		r.ServeHTTP(w1, req1)
		return w1
	}())

	assert.Equal(t, http.StatusOK, w1.Code)

	// User 2 on Tenant 2 with the same Idempotency-Key
	req2 := httptest.NewRequest(http.MethodPost, "/test", bytes.NewBufferString("{}"))
	req2.Header.Set("Idempotency-Key", "iso-key")
	w2 := httptest.NewRecorder()

	r.ServeHTTP(w2, func() *httptest.ResponseRecorder {
		ctx, _ := gin.CreateTestContext(w2)
		ctx.Request = req2
		ctx.Set("tenantID", "tenant-2")
		ctx.Set("callerID", "caller-2")
		r.ServeHTTP(w2, req2)
		return w2
	}())

	// Should execute the handler because they are scoped to different users/tenants
	assert.Equal(t, http.StatusOK, w2.Code)
	var resp2 map[string]interface{}
	err := json.Unmarshal(w2.Body.Bytes(), &resp2)
	require.NoError(t, err)
	assert.Equal(t, float64(2), resp2["count"]) // Count is incremented to 2 (executed!)
}

func TestInMemoryIdempotencyStore_Expiration(t *testing.T) {
	store := middleware.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	// 1st request insert
	statusCode, body, isReplay, isInFlight, err := store.GetOrInsert(
		ctx, "scope1", "key1", "POST", "/path", "hash1", 5*time.Millisecond,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, statusCode)
	assert.False(t, isReplay)
	assert.False(t, isInFlight)

	// Update response
	err = store.UpdateResponse(ctx, "scope1", "key1", 200, []byte("response"))
	require.NoError(t, err)

	// Retrieve should replay
	statusCode, body, isReplay, isInFlight, err = store.GetOrInsert(
		ctx, "scope1", "key1", "POST", "/path", "hash1", 5*time.Millisecond,
	)
	require.NoError(t, err)
	assert.Equal(t, 200, statusCode)
	assert.Equal(t, []byte("response"), body)
	assert.True(t, isReplay)

	// Wait for expiration
	time.Sleep(10 * time.Millisecond)

	// Retrieve again: should act as first request
	statusCode, body, isReplay, isInFlight, err = store.GetOrInsert(
		ctx, "scope1", "key1", "POST", "/path", "hash1", 5*time.Millisecond,
	)
	require.NoError(t, err)
	assert.Equal(t, 0, statusCode)
	assert.False(t, isReplay)
	assert.False(t, isInFlight)
}

func TestInMemoryIdempotencyStore_Delete(t *testing.T) {
	store := middleware.NewInMemoryIdempotencyStore()
	ctx := context.Background()

	_, _, _, _, err := store.GetOrInsert(ctx, "scope1", "key1", "POST", "/path", "hash1", time.Hour)
	require.NoError(t, err)

	err = store.Delete(ctx, "scope1", "key1")
	require.NoError(t, err)

	statusCode, _, isReplay, _, err := store.GetOrInsert(ctx, "scope1", "key1", "POST", "/path", "hash1", time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, statusCode)
	assert.False(t, isReplay)
}

func TestPostgresIdempotencyStore_NilPool(t *testing.T) {
	store := middleware.NewPostgresIdempotencyStore(nil)
	ctx := context.Background()

	_, _, _, _, err := store.GetOrInsert(ctx, "scope", "key", "POST", "/path", "hash", time.Hour)
	assert.Error(t, err)

	err = store.UpdateResponse(ctx, "scope", "key", 200, []byte{})
	assert.Error(t, err)

	err = store.Delete(ctx, "scope", "key")
	assert.Error(t, err)
}
