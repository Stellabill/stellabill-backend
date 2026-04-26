package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRequestSizeLimit_WithinLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(100))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	body := bytes.Repeat([]byte("a"), 50)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["received"] != 50 {
		t.Fatalf("expected received=50, got %d", resp["received"])
	}
}

func TestRequestSizeLimit_AtExactLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(100))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	body := bytes.Repeat([]byte("a"), 100)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_ExceedsLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(100))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"should_not_reach": true})
	})

	body := bytes.Repeat([]byte("a"), 101)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if resp["error"] != "request_too_large" {
		t.Fatalf("expected error='request_too_large', got %v", resp)
	}
	if int64(resp["max_bytes"].(float64)) != 100 {
		t.Fatalf("expected max_bytes=100, got %v", resp["max_bytes"])
	}
}

func TestRequestSizeLimit_ZeroLimit_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(0))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	body := bytes.Repeat([]byte("a"), 1000)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 with zero limit (no limit), got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_NegativeLimit_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(-1))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	body := bytes.Repeat([]byte("a"), 500)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 with negative limit (no limit), got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(100))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(nil))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty body, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["received"] != 0 {
		t.Fatalf("expected received=0, got %d", resp["received"])
	}
}

func TestRequestSizeLimit_OneByteOver(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(10))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler should not be called when limit exceeded")
	})

	body := []byte("abcdefghijk")
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for one byte over, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_PerRouteOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.POST("/small", RequestSizeLimit(5), func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})
	router.POST("/large", RequestSizeLimit(100), func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	t.Run("small limit rejects 10 bytes", func(t *testing.T) {
		body := bytes.Repeat([]byte("a"), 10)
		req := httptest.NewRequest(http.MethodPost, "/small", bytes.NewReader(body))
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("expected 413 for small limit route, got %d body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("small limit accepts 3 bytes", func(t *testing.T) {
		body := bytes.Repeat([]byte("a"), 3)
		req := httptest.NewRequest(http.MethodPost, "/small", bytes.NewReader(body))
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 for small limit route with 3 bytes, got %d body=%s", res.Code, res.Body.String())
		}
	})

	t.Run("large limit accepts 50 bytes", func(t *testing.T) {
		body := bytes.Repeat([]byte("a"), 50)
		req := httptest.NewRequest(http.MethodPost, "/large", bytes.NewReader(body))
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("expected 200 for large limit route with 50 bytes, got %d body=%s", res.Code, res.Body.String())
		}
	})
}

func TestRequestSizeLimit_ChunkedEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(50))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	pr, pw := io.Pipe()
	go func() {
		pw.Write([]byte("chunk1"))
		pw.Write([]byte("chunk2"))
		pw.Close()
	}()

	req := httptest.NewRequest(http.MethodPost, "/test", pr)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()

	go func() {
		router.ServeHTTP(res, req)
	}()

	time.Sleep(100 * time.Millisecond)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for chunked body within limit, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_BodyReadError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(100))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"should_not_reach": true})
	})

	pr, pw := io.Pipe()
	pw.CloseWithError(io.ErrUnexpectedEOF)

	req := httptest.NewRequest(http.MethodPost, "/test", pr)
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for body read error, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_MultipleRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(50))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	for i := 0; i < 10; i++ {
		body := bytes.Repeat([]byte("a"), i)
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		expectedStatus := http.StatusOK
		if i > 50 {
			expectedStatus = http.StatusRequestEntityTooLarge
		}
		if res.Code != expectedStatus {
			t.Fatalf("request %d: expected %d, got %d body=%s", i, expectedStatus, res.Code, res.Body.String())
		}
	}
}

func TestRequestSizeLimit_LargeRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(1024*1024))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	body := bytes.Repeat([]byte("a"), 1024*1024)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for 1MB request at 1MB limit, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_GzipCompressed(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(100))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	compressed := []byte{0x1f, 0x8b, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for gzip request (middleware does not decompress), got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_GetRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(10))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET request, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRequestSizeLimit_PreservesRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(1000))
	router.POST("/test", func(c *gin.Context) {
		body1, _ := io.ReadAll(c.Request.Body)
		body2, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{
			"first_read":  len(body1),
			"second_read": len(body2),
			"body_match":  bytes.Equal(body1, body2),
		})
	})

	body := []byte(`{"key":"value"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if int(resp["first_read"].(float64)) != 15 {
		t.Fatalf("expected first_read=15, got %v", resp["first_read"])
	}
	if int(resp["second_read"].(float64)) != 0 {
		t.Fatalf("expected second_read=0 (body exhausted), got %v", resp["second_read"])
	}
}

func TestRequestSizeLimit_JsonContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(RequestSizeLimit(50))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	jsonBody := []byte(`{"name":"test","data":"value"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for JSON content type, got %d body=%s", res.Code, res.Body.String())
	}
}
