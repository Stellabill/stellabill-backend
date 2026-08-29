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

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
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

	done := make(chan struct{})
	go func() {
		router.ServeHTTP(res, req)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for chunked gzip request")
	}
	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for chunked gzip, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestCompressionResponse_Zstd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain", []byte(strings.Repeat("hello world", 100)))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "zstd")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if res.Header().Get("Content-Encoding") != "zstd" {
		t.Fatalf("expected Content-Encoding: zstd, got %s", res.Header().Get("Content-Encoding"))
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("create zstd reader: %v", err)
	}
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(res.Body.Bytes(), nil)
	if err != nil {
		t.Fatalf("decompress zstd: %v", err)
	}
	if expected := strings.Repeat("hello world", 100); string(decoded) != expected {
		t.Fatalf("expected %q, got %q", expected, string(decoded))
	}
}

func TestCompressionResponse_Brotli(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain", []byte(strings.Repeat("hello world", 100)))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "br")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if res.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("expected Content-Encoding: br, got %s", res.Header().Get("Content-Encoding"))
	}

	decoded, err := io.ReadAll(brotli.NewReader(res.Body))
	if err != nil {
		t.Fatalf("decompress brotli: %v", err)
	}
	if expected := strings.Repeat("hello world", 100); string(decoded) != expected {
		t.Fatalf("expected %q, got %q", expected, string(decoded))
	}
}

func TestCompressionResponse_Gzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain", []byte(strings.Repeat("hello world", 100)))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if res.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %s", res.Header().Get("Content-Encoding"))
	}

	zr, err := gzip.NewReader(res.Body)
	if err != nil {
		t.Fatalf("create gzip reader: %v", err)
	}
	defer zr.Close()
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompress gzip: %v", err)
	}
	if expected := strings.Repeat("hello world", 100); string(decoded) != expected {
		t.Fatalf("expected %q, got %q", expected, string(decoded))
	}
}

func TestCompressionResponse_Negotiation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		accept   string
		expected string
	}{
		{"prefer zstd over br", "zstd, br, gzip", "zstd"},
		{"prefer br over gzip", "br, gzip", "br"},
		{"gzip only", "gzip", "gzip"},
		{"zstd with quality", "zstd;q=1.0, br;q=0.8", "zstd"},
		{"no encoding", "", ""},
		{"identity", "identity", ""},
		{"wildcard should not match", "*/*", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := gin.New()
			router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
			router.GET("/test", func(c *gin.Context) {
				c.Data(http.StatusOK, "text/plain", []byte(strings.Repeat("x", 500)))
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Accept-Encoding", tt.accept)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if tt.expected == "" {
				if res.Header().Get("Content-Encoding") != "" {
					t.Errorf("expected no Content-Encoding, got %s", res.Header().Get("Content-Encoding"))
				}
			} else {
				if res.Header().Get("Content-Encoding") != tt.expected {
					t.Errorf("expected Content-Encoding: %s, got %s", tt.expected, res.Header().Get("Content-Encoding"))
				}
			}
		})
	}
}

func TestCompressionResponse_MinSize(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 500}))
	router.GET("/small", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain", []byte("small body"))
	})
	router.GET("/large", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/plain", []byte(strings.Repeat("large body ", 100)))
	})

	t.Run("small body not compressed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/small", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Header().Get("Content-Encoding") != "" {
			t.Fatalf("expected no compression for small body, got %s", res.Header().Get("Content-Encoding"))
		}
		if body := res.Body.String(); body != "small body" {
			t.Fatalf("expected 'small body', got %q", body)
		}
	})

	t.Run("large body compressed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/large", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("expected gzip compression for large body, got %s", res.Header().Get("Content-Encoding"))
		}
		zr, _ := gzip.NewReader(res.Body)
		decoded, _ := io.ReadAll(zr)
		zr.Close()
		if expected := strings.Repeat("large body ", 100); string(decoded) != expected {
			t.Fatalf("decompressed body mismatch")
		}
	})
}

func TestCompressionResponse_SkipCompressedContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []string{
		"image/png",
		"video/mp4",
		"audio/mpeg",
		"application/zip",
		"application/pdf",
		"font/woff2",
	}

	for _, ct := range tests {
		t.Run(ct, func(t *testing.T) {
			router := gin.New()
			router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
			router.GET("/test", func(c *gin.Context) {
				c.Data(http.StatusOK, ct, []byte(strings.Repeat("x", 1000)))
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Accept-Encoding", "gzip")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)

			if res.Header().Get("Content-Encoding") != "" {
				t.Fatalf("expected no compression for %s, got Content-Encoding: %s", ct, res.Header().Get("Content-Encoding"))
			}
		})
	}
}

func TestCompressionResponse_SkipWhenContentEncodingSet(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Header("Content-Encoding", "identity")
		c.Data(http.StatusOK, "text/plain", []byte(strings.Repeat("x", 1000)))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "identity" {
		t.Fatalf("expected Content-Encoding: identity, got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestCompressionResponse_RequestDecompressionStillWorks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 10000,
		ResponseCompression:  true,
		MinCompressBytes:     1,
	}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.Data(http.StatusOK, "text/plain", append([]byte("echo: "), body...))
	})

	original := []byte(`{"hello":"world"}`)
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	w.Write(original)
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Accept-Encoding", "zstd")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}

	if res.Header().Get("Content-Encoding") != "zstd" {
		t.Fatalf("expected Content-Encoding: zstd, got %s", res.Header().Get("Content-Encoding"))
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatalf("create zstd reader: %v", err)
	}
	defer decoder.Close()
	decoded, err := decoder.DecodeAll(res.Body.Bytes(), nil)
	if err != nil {
		t.Fatalf("decompress zstd: %v", err)
	}
	if expected := "echo: " + string(original); string(decoded) != expected {
		t.Fatalf("expected %q, got %q", expected, string(decoded))
	}
}

func TestCompressionResponse_BackwardCompatibleDefault(t *testing.T) {
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
		t.Fatalf("expected 200, got %d", res.Code)
	}
}

func BenchmarkCompressionResponse_Zstd(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := []byte(strings.Repeat(`{"key":"value","data":"benchmark test payload"}`, 200))

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", payload)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "zstd")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", res.Code)
		}
	}
}

func BenchmarkCompressionResponse_Brotli(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := []byte(strings.Repeat(`{"key":"value","data":"benchmark test payload"}`, 200))

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", payload)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "br")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", res.Code)
		}
	}
}

func BenchmarkCompressionResponse_Gzip(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := []byte(strings.Repeat(`{"key":"value","data":"benchmark test payload"}`, 200))

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{ResponseCompression: true, MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", payload)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			b.Fatalf("unexpected status: %d", res.Code)
		}
	}
}

// ─── gzip-bomb defense tests ────────────────────────────────────────────────
//
// These tests craft high-ratio gzip payloads and verify that the middleware
// aborts decompression before exhausting memory, returning 413 without
// allocating the full expanded body.

// makeGzipBomb compresses `decompressedSize` zero-bytes into a gzip stream.
// Returns (compressedBody []byte, decompressedSize int).
// The produced stream is a true gzip bomb: very small compressed, very large
// when expanded.
func makeGzipBomb(t testing.TB, decompressedSize int) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	chunk := make([]byte, 32*1024) // 32 KB zero-filled chunks
	remaining := decompressedSize
	for remaining > 0 {
		n := remaining
		if n > len(chunk) {
			n = len(chunk)
		}
		if _, err := w.Write(chunk[:n]); err != nil {
			t.Fatalf("makeGzipBomb: gzip write: %v", err)
		}
		remaining -= n
	}
	if err := w.Close(); err != nil {
		t.Fatalf("makeGzipBomb: gzip close: %v", err)
	}
	return buf.Bytes()
}

// makeGzipOf compresses arbitrary plaintext.
func makeGzipOf(t testing.TB, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(plain); err != nil {
		t.Fatalf("makeGzipOf: write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("makeGzipOf: close: %v", err)
	}
	return buf.Bytes()
}

// TestGzipPolicy_LargeZeroBomb_DecodedCap verifies that a gzip stream which
// would expand to 10 MB of zeroes is blocked by the MaxUncompressedBytes cap.
// Memory growth must stay bounded to roughly maxCap, not 10 MB.
func TestGzipPolicy_LargeZeroBomb_DecodedCap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const bombSize = 10 * 1024 * 1024  // 10 MB expanded
	const maxDecoded = 512 * 1024      // 512 KB decoded cap
	const maxCompressed = 1024 * 1024  // 1 MB compressed cap (bomb is <100 KB)

	compressed := makeGzipBomb(t, bombSize)
	if int64(len(compressed)) > maxCompressed {
		t.Fatalf("test setup: compressed bomb (%d B) exceeds maxCompressed (%d B)", len(compressed), maxCompressed)
	}

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: maxDecoded,
		MaxCompressedBytes:   maxCompressed,
		MaxRatio:             0, // disabled; rely on absolute cap only
	}))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler must not be reached for a gzip bomb")
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for 10MB gzip bomb, got %d body=%s", res.Code, res.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != "decompression_bomb" {
		t.Fatalf("expected error='decompression_bomb', got %v", resp)
	}
	// The response must report that the decoded cap was hit, not a compressed-size error.
	if _, ok := resp["max_uncompressed"]; !ok {
		t.Fatalf("expected 'max_uncompressed' field in error response, got %v", resp)
	}
}

// TestGzipPolicy_LargeZeroBomb_RatioCap verifies the same 10 MB bomb is
// stopped by the ratio cap when the absolute decoded cap is not the tightest
// constraint.
func TestGzipPolicy_LargeZeroBomb_RatioCap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const bombSize = 10 * 1024 * 1024 // 10 MB expanded
	const maxRatio = 20.0              // ratio cap; bomb typically exceeds 100:1

	compressed := makeGzipBomb(t, bombSize)
	// MaxUncompressedBytes is set large enough that only the ratio cap triggers.
	maxDecoded := int64(len(compressed)) * int64(maxRatio) // exactly ratio * compressed
	// We need maxDecoded to be well below the bomb's decoded size.
	if maxDecoded >= int64(bombSize) {
		// ratio cap alone won't trigger; skip ratio-only path for this test.
		t.Skip("compressed body too large; ratio cap does not kick in for this payload size")
	}

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: int64(bombSize) + 1024*1024, // very generous absolute cap
		MaxCompressedBytes:   int64(len(compressed)) + 1024*1024,
		MaxRatio:             maxRatio,
	}))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler must not be reached for a gzip ratio bomb")
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for ratio-bomb, got %d body=%s", res.Code, res.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != "decompression_bomb" {
		t.Fatalf("expected error='decompression_bomb', got %v", resp)
	}
}

// TestGzipPolicy_ExactlyAtDecodedCap verifies that a payload whose decompressed
// size equals MaxUncompressedBytes exactly is allowed through.
func TestGzipPolicy_ExactlyAtDecodedCap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const cap = 1024 // 1 KB

	plain := bytes.Repeat([]byte("A"), cap)
	compressed := makeGzipOf(t, plain)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: cap,
		MaxRatio:             0, // no ratio limit
	}))
	router.POST("/test", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Fatalf("handler: read body: %v", err)
		}
		if len(body) != cap {
			t.Fatalf("handler: expected %d bytes, got %d", cap, len(body))
		}
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for payload exactly at decoded cap, got %d body=%s", res.Code, res.Body.String())
	}
}

// TestGzipPolicy_JustOverDecodedCap verifies that a payload whose decompressed
// size is exactly MaxUncompressedBytes+1 is rejected with 413.
func TestGzipPolicy_JustOverDecodedCap(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const cap = 1024 // 1 KB decoded cap

	plain := bytes.Repeat([]byte("A"), cap+1) // one byte over
	compressed := makeGzipOf(t, plain)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: cap,
		MaxRatio:             0, // no ratio limit
	}))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler must not be reached when decoded cap is exceeded")
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for payload one byte over decoded cap, got %d body=%s", res.Code, res.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != "decompression_bomb" {
		t.Fatalf("expected error='decompression_bomb', got %v", resp)
	}
}

// TestGzipPolicy_MaxCompressedBytes_Enforced verifies that a gzip body whose
// compressed size exceeds MaxCompressedBytes is rejected with 413 before
// decompression is attempted.
func TestGzipPolicy_MaxCompressedBytes_Enforced(t *testing.T) {
	gin.SetMode(gin.TestMode)

	plain := bytes.Repeat([]byte("hello "), 500) // ~3 KB plain
	compressed := makeGzipOf(t, plain)

	// Allow the uncompressed cap to be huge; only the compressed cap should fire.
	maxCompressed := int64(len(compressed)) - 1 // one byte under compressed size

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 1024 * 1024, // 1 MB — generous decoded cap
		MaxCompressedBytes:   maxCompressed,
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler must not be reached when compressed cap is exceeded")
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 when compressed body exceeds MaxCompressedBytes, got %d body=%s", res.Code, res.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != "request_too_large" {
		t.Fatalf("expected error='request_too_large', got %v", resp)
	}
}

// TestGzipPolicy_MaxCompressedBytes_ExactLimit verifies that a gzip body
// exactly at MaxCompressedBytes passes through correctly.
func TestGzipPolicy_MaxCompressedBytes_ExactLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	plain := []byte("exact compressed limit test payload")
	compressed := makeGzipOf(t, plain)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 1024,
		MaxCompressedBytes:   int64(len(compressed)), // exactly at limit
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for body exactly at MaxCompressedBytes, got %d body=%s", res.Code, res.Body.String())
	}
}

// TestGzipPolicy_NestedGzip verifies that a gzip stream wrapping another gzip
// stream (double-compressed) is not recursively decompressed — the inner gzip
// bytes are passed to the handler as-is.
func TestGzipPolicy_NestedGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	inner := makeGzipOf(t, []byte("inner payload"))
	outer := makeGzipOf(t, inner) // outer gzip wraps inner gzip bytes

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 64 * 1024,
		MaxRatio:             100,
	}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		// The handler receives the inner gzip bytes, not "inner payload".
		// Verify it looks like a valid gzip stream.
		if len(body) < 10 || body[0] != 0x1f || body[1] != 0x8b {
			t.Fatalf("expected inner gzip magic bytes, got %x", body[:min(10, len(body))])
		}
		c.JSON(http.StatusOK, gin.H{"inner_size": len(body)})
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(outer))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for nested gzip (no recursive decompression), got %d body=%s", res.Code, res.Body.String())
	}
}

// TestGzipPolicy_MissingContentEncoding_PassesThrough verifies that a plain
// body without Content-Encoding is never decompressed, even if the bytes happen
// to look like gzip (magic bytes present). Backward compat check.
func TestGzipPolicy_MissingContentEncoding_PassesThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A real gzip stream but sent without Content-Encoding.
	compressed := makeGzipOf(t, []byte("plain passthrough"))

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 64 * 1024,
		MaxRatio:             10,
	}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		// Handler receives raw gzip bytes, not decompressed content.
		if len(body) == 0 {
			t.Fatal("expected non-empty body")
		}
		c.JSON(http.StatusOK, gin.H{"raw_size": len(body)})
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	// No Content-Encoding header set intentionally.
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for request with no Content-Encoding, got %d body=%s", res.Code, res.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if int(resp["raw_size"].(float64)) != len(compressed) {
		t.Fatalf("expected raw_size=%d, got %v", len(compressed), resp["raw_size"])
	}
}

// TestGzipPolicy_HighRatioSmall_Blocked verifies a small payload with
// extremely high compression ratio (10 000 × ) is caught by the ratio cap.
func TestGzipPolicy_HighRatioSmall_Blocked(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const repeats = 10_000
	const maxRatio = 50.0

	plain := bytes.Repeat([]byte("Z"), repeats) // 10 KB; ratio >> 50
	compressed := makeGzipOf(t, plain)

	// Sanity: ratio must exceed maxRatio for this test to be meaningful.
	actualRatio := float64(len(plain)) / float64(len(compressed))
	if actualRatio <= maxRatio {
		t.Skipf("compression ratio %.1f <= maxRatio %.1f; skipping ratio-cap test", actualRatio, maxRatio)
	}

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: int64(repeats) + 1024*1024, // generous absolute cap
		MaxCompressedBytes:   int64(len(compressed)) + 1024*1024,
		MaxRatio:             maxRatio,
	}))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler must not be reached when ratio cap is exceeded")
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for high-ratio bomb (ratio=%.1f, max=%.1f), got %d body=%s",
			actualRatio, maxRatio, res.Code, res.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != "decompression_bomb" {
		t.Fatalf("expected error='decompression_bomb', got %v", resp)
	}
}

// TestGzipPolicy_AllLimitsDisabled_LargeBombPasses verifies that when all
// limits are set to zero the middleware imposes no restriction and even a
// large payload passes (relevant for opt-out scenarios).
func TestGzipPolicy_AllLimitsDisabled_LargeBombPasses(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large decompression in -short mode")
	}
	gin.SetMode(gin.TestMode)
	const bombSize = 2 * 1024 * 1024 // 2 MB — keep reasonable for CI
	compressed := makeGzipBomb(t, bombSize)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 0,
		MaxCompressedBytes:   0,
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"received": len(body)})
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 with all limits disabled, got %d body=%s", res.Code, res.Body.String())
	}
}

// TestGzipPolicy_RequestSizeMiddleware_GzipBodyRejected tests the interaction
// between RequestSizeLimit and GzipPolicy: a small compressed body that would
// expand massively should be caught by GzipPolicy, not RequestSizeLimit.
func TestGzipPolicy_RequestSizeMiddleware_GzipBodyRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const bombExpanded = 1 * 1024 * 1024 // 1 MB expanded
	const maxCompressed = 512 * 1024     // 512 KB compressed cap
	const maxDecoded = 256 * 1024        // 256 KB decoded cap

	compressed := makeGzipBomb(t, bombExpanded)

	router := gin.New()
	// RequestSizeLimit runs first; it only sees the compressed payload.
	router.Use(RequestSizeLimit(maxCompressed))
	// GzipPolicy then enforces the decoded cap.
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: maxDecoded,
		MaxCompressedBytes:   maxCompressed,
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler must not be reached")
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 for gzip bomb through middleware chain, got %d body=%s", res.Code, res.Body.String())
	}
}

// TestRequestSizeLimit_GzipBomb_CompressedSize tests that RequestSizeLimit
// correctly rejects a large compressed gzip body based on its wire size.
func TestRequestSizeLimit_GzipBomb_CompressedSize(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const bombExpanded = 5 * 1024 * 1024 // 5 MB expanded

	compressed := makeGzipBomb(t, bombExpanded)
	// Set limit to exactly the compressed size minus 1 byte.
	limit := int64(len(compressed)) - 1

	router := gin.New()
	router.Use(RequestSizeLimit(limit))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler must not be reached")
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413 when compressed bomb exceeds RequestSizeLimit, got %d body=%s", res.Code, res.Body.String())
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp["error"] != "request_too_large" {
		t.Fatalf("expected error='request_too_large', got %v", resp)
	}
}

// TestRequestSizeLimit_GzipBomb_CompressedAtLimit tests that RequestSizeLimit
// accepts a compressed gzip body exactly at the limit.
func TestRequestSizeLimit_GzipBomb_CompressedAtLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const bombExpanded = 1 * 1024 * 1024 // 1 MB expanded

	compressed := makeGzipBomb(t, bombExpanded)
	limit := int64(len(compressed)) // exactly at limit

	router := gin.New()
	router.Use(RequestSizeLimit(limit))
	// GzipPolicy with a large decoded cap so only the wire size is tested here.
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 2 * 1024 * 1024,
		MaxCompressedBytes:   limit,
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
	req.Header.Set("Content-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200 for compressed body exactly at RequestSizeLimit, got %d body=%s", res.Code, res.Body.String())
	}
}

// TestGzipPolicy_ConcurrentBombs verifies that concurrent requests each
// carrying a gzip bomb are all rejected safely (no race, no goroutine leak).
func TestGzipPolicy_ConcurrentBombs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const concurrency = 10
	const bombExpanded = 512 * 1024 // 512 KB per bomb
	const maxDecoded = 64 * 1024    // 64 KB decoded cap

	compressed := makeGzipBomb(t, bombExpanded)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: maxDecoded,
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		t.Fatal("handler must not be reached for a gzip bomb")
	})

	results := make(chan int, concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
			req.Header.Set("Content-Encoding", "gzip")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			results <- res.Code
		}()
	}

	timeout := time.After(10 * time.Second)
	for i := 0; i < concurrency; i++ {
		select {
		case code := <-results:
			if code != http.StatusRequestEntityTooLarge {
				t.Errorf("goroutine %d: expected 413, got %d", i, code)
			}
		case <-timeout:
			t.Fatal("timed out waiting for concurrent bomb results")
		}
	}
}

// ─── Benchmarks ─────────────────────────────────────────────────────────────

// BenchmarkGzipBomb_Detection measures how quickly the middleware detects and
// rejects a 5 MB gzip bomb, and ensures memory growth is bounded.
func BenchmarkGzipBomb_Detection(b *testing.B) {
	gin.SetMode(gin.TestMode)
	const bombExpanded = 5 * 1024 * 1024 // 5 MB expanded
	const maxDecoded = 64 * 1024         // 64 KB decoded cap

	compressed := makeGzipBomb(b, bombExpanded)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: maxDecoded,
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		b.Fatal("handler must not be reached")
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
		req.Header.Set("Content-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusRequestEntityTooLarge {
			b.Fatalf("expected 413, got %d", res.Code)
		}
	}
}

// BenchmarkGzipBomb_CompressedSizeCap measures rejection at the compressed-size
// gate (before decompression even starts).
func BenchmarkGzipBomb_CompressedSizeCap(b *testing.B) {
	gin.SetMode(gin.TestMode)
	const bombExpanded = 5 * 1024 * 1024 // 5 MB expanded
	compressed := makeGzipBomb(b, bombExpanded)
	maxCompressed := int64(len(compressed)) - 1

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 64 * 1024,
		MaxCompressedBytes:   maxCompressed,
		MaxRatio:             0,
	}))
	router.POST("/test", func(c *gin.Context) {
		b.Fatal("handler must not be reached")
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
		req.Header.Set("Content-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusRequestEntityTooLarge {
			b.Fatalf("expected 413, got %d", res.Code)
		}
	}
}

// BenchmarkGzipBomb_LegitimateSmallPayload measures overhead for a normal
// small request that should pass through without issues.
func BenchmarkGzipBomb_LegitimateSmallPayload(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := []byte(`{"subscription_id":"sub-123","plan":"pro","amount":2999}`)
	compressed := makeGzipOf(b, payload)

	router := gin.New()
	router.Use(GzipPolicy(GzipPolicyConfig{
		MaxUncompressedBytes: 1024 * 1024,
		MaxCompressedBytes:   512 * 1024,
		MaxRatio:             50,
	}))
	router.POST("/test", func(c *gin.Context) {
		io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/test", bytes.NewReader(compressed))
		req.Header.Set("Content-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
		if res.Code != http.StatusOK {
			b.Fatalf("expected 200, got %d", res.Code)
		}
	}
}

// helper: min for integers (Go 1.21+ has builtin but keep compat)
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
