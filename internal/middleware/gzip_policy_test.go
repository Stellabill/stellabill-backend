package middleware

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestGzipPolicy_NoEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	body := []byte(`{"test":"data"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for no encoding, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["received"] != 15 {
		t.Fatalf("expected received=15, got %d", resp["received"])
	}
}

func TestGzipPolicy_IdentityEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	body := []byte(`{"test":"data"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "identity")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for identity encoding, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestGzipPolicy_ValidGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body), "data": string(body)})
	})

	original := []byte(`{"test":"hello world"}`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid gzip, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["data"] != `{"test":"hello world"}` {
		t.Fatalf("expected decompressed JSON, got %v", resp["data"])
	}
}

func TestGzipPolicy_DeflateRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"should_not": "reach"})
	})

	body := []byte(`test data`)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Encoding", "deflate")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotAcceptable {
		t.Fatalf("expected 406 for deflate encoding, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "unsupported_encoding" {
		t.Fatalf("expected error='unsupported_encoding', got %v", resp)
	}
	if resp["encoding"] != "deflate" {
		t.Fatalf("expected encoding='deflate', got %v", resp["encoding"])
	}
}

func TestGzipPolicy_BrotliRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"should_not": "reach"})
	})

	body := []byte(`test data`)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Encoding", "br")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotAcceptable {
		t.Fatalf("expected 406 for br encoding, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "unsupported_encoding" {
		t.Fatalf("expected error='unsupported_encoding', got %v", resp)
	}
	if resp["encoding"] != "br" {
		t.Fatalf("expected encoding='br', got %v", resp["encoding"])
	}
}

func TestGzipPolicy_UnknownEncodingRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []string{"zstd", "lzma", "bz2", "xz", "snappy"}

	for _, enc := range testCases {
		router := gin.New()
		router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"should_not": "reach"})
		})

		body := []byte(`test data`)
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
		req.Header.Set("Content-Encoding", enc)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Code != http.StatusNotAcceptable {
			t.Fatalf("encoding %s: expected 406, got %d body=%s", enc, res.Code, res.Body.String())
		}
	}
}

func TestGzipPolicy_InvalidGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"should_not": "reach"})
	})

	body := []byte(`not gzip data`)
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(body))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid gzip, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "invalid_gzip" {
		t.Fatalf("expected error='invalid_gzip', got %v", resp)
	}
}

func TestGzipPolicy_TruncatedGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	original := []byte(`{"test":"hello world"}`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()
	gzipData := buf.Bytes()

	truncated := gzipData[:len(gzipData)/2]
	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(truncated))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for truncated gzip (valid partial content), got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["received"] == 0 {
		t.Fatalf("expected some bytes decompressed, got %d", resp["received"])
	}
}

func TestGzipPolicy_MixedCaseEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	testCases := []string{"GZIP", "Gzip", "GZip", "gZIP"}

	for _, enc := range testCases {
		router := gin.New()
		router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
		router.POST("/test", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"status": "ok"})
		})

		original := []byte(`{"test":"data"}`)
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		w.Write(original)
		w.Close()

		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
		req.Header.Set("Content-Encoding", enc)
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Code != http.StatusOK {
			t.Fatalf("encoding %s: expected 200, got %d body=%s", enc, res.Code, res.Body.String())
		}
	}
}

func TestGzipPolicy_WithWhitespaceInEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	original := []byte(`{"test":"data"}`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", " gzip ")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for gzip with whitespace, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestGzipPolicy_EmptyBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{MaxUncompressedBytes: 100}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty gzip body, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["received"] != 0 {
		t.Fatalf("expected received=0, got %d", resp["received"])
	}
}

func TestGzipPolicy_CompressionRatioBomb(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 100,
		MaxRatio:             5.0,
	}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"should_not": "reach"})
	})

	highlyCompressible := bytes.Repeat([]byte("AAAA"), 100)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(highlyCompressible)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for ratio bomb, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "decompression_bomb" {
		t.Fatalf("expected error='decompression_bomb', got %v", resp)
	}
}

func TestGzipPolicy_AbsoluteSizeBomb(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 50,
	}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"should_not": "reach"})
	})

	original := []byte(strings.Repeat("AAAA", 100))
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for absolute size bomb, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "decompression_bomb" {
		t.Fatalf("expected error='decompression_bomb', got %v", resp)
	}
}

func TestGzipPolicy_SmallCompressedPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 100,
		MaxRatio:             10.0,
	}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	original := []byte(`small payload`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for small compressed payload, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]int
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["received"] != 13 {
		t.Fatalf("expected received=13, got %d", resp["received"])
	}
}

func TestGzipPolicy_CompressedOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 10,
	}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"should_not": "reach"})
	})

	original := []byte(`{"test":"hello world"}`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for compressed over limit, got %d body=%s", res.Code, res.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["error"] != "request_too_large" {
		t.Fatalf("expected error='request_too_large', got %v", resp)
	}
}

func TestGzipPolicy_ZerorMaxUncompressed_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 0,
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	original := []byte(`{"test":"hello world"}`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 with zero limits (no limit), got %d body=%s", res.Code, res.Body.String())
	}
}

func TestGzipPolicy_NegativeMaxRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 100,
		MaxRatio:             -1,
	}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	original := []byte(`{"test":"hello world"}`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 with negative ratio (no ratio limit), got %d body=%s", res.Code, res.Body.String())
	}
}

func TestGzipPolicy_GetRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 100,
	}))
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

func TestGzipPolicy_PreservesRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 1000,
		MaxRatio:             100,
	}))
	router.POST("/test", func(c *gin.Context) {
		body1, _ := io.ReadAll(c.Request.Body)
		body2, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{
			"first_read":  len(body1),
			"second_read": len(body2),
			"body_match":  bytes.Equal(body1, body2),
		})
	})

	original := []byte(`{"key":"value"}`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
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

func TestGzipPolicy_OptionsRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 100,
	}))
	router.OPTIONS("/test", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS request, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestGzipPolicy_ChunkedTransfer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 100,
		MaxRatio:             10,
	}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	pr, pw := io.Pipe()
	go func() {
		w := gzip.NewWriter(pw)
		w.Write([]byte(`chunked data`))
		w.Close()
		pw.Close()
	}()

	req := httptest.NewRequest(http.MethodPost, "/test", pr)
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()

	go func() {
		router.ServeHTTP(res, req)
	}()

	time.Sleep(100 * time.Millisecond)
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for chunked gzip, got %d body=%s", res.Code, res.Body.String())
	}
}
