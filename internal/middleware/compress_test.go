package middleware

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

// ---- Helper functions ----

func decodeGzipBody(t *testing.T, data []byte) string {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	defer r.Close()
	body, _ := io.ReadAll(r)
	return string(body)
}

func decodeZstdBody(t *testing.T, data []byte) string {
	t.Helper()
	r, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer r.Close()
	body, _ := io.ReadAll(r)
	return string(body)
}

func decodeBrotliBody(t *testing.T, data []byte) string {
	t.Helper()
	r := brotli.NewReader(bytes.NewReader(data))
	body, _ := io.ReadAll(r)
	return string(body)
}

func largePayload(size int) []byte {
	return []byte(strings.Repeat(`{"key":"value","nested":{"a":"b"}}`, size))
}

// ---- Tests ----

func TestCompressResponse_NoAcceptEncoding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(DefaultCompressConfig()))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No Accept-Encoding header
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", res.Code)
	}
	if res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding, got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestCompressResponse_Gzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
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
	decoded := decodeGzipBody(t, res.Body.Bytes())
	if !strings.Contains(decoded, "data") {
		t.Fatalf("expected decoded body to contain 'data', got: %s", decoded[:min(50, len(decoded))])
	}
}

func TestCompressResponse_Zstd(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
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
	decoded := decodeZstdBody(t, res.Body.Bytes())
	if !strings.Contains(decoded, "data") {
		t.Fatalf("expected decoded body to contain 'data', got: %s", decoded[:min(50, len(decoded))])
	}
}

func TestCompressResponse_Brotli(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
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
	decoded := decodeBrotliBody(t, res.Body.Bytes())
	if !strings.Contains(decoded, "data") {
		t.Fatalf("expected decoded body to contain 'data', got: %s", decoded[:min(50, len(decoded))])
	}
}

func TestCompressResponse_PriorityZstdOverGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
	})

	// Client accepts both — should pick zstd (highest priority).
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip, br, zstd")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "zstd" {
		t.Fatalf("expected Content-Encoding: zstd (highest priority), got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestCompressResponse_PriorityBrotliOverGzip(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("expected Content-Encoding: br, got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestCompressResponse_QualityFactor(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
	})

	// Client prefers gzip over zstd via quality.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "zstd;q=0.3, gzip;q=0.8")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip (higher q), got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestCompressResponse_IdentityQZero(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
	})

	// identity;q=0 means "send uncompressed".
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "identity;q=0")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding for identity;q=0, got %s", res.Header().Get("Content-Encoding"))
	}
	// Verify the uncompressed body is valid JSON.
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
}

func TestCompressResponse_SmallPayloadNoCompression(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1024}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "short"})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip, zstd")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding for small payload, got %s", res.Header().Get("Content-Encoding"))
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
}

func TestCompressResponse_StatusCodePreserved(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/notfound", func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	})

	req := httptest.NewRequest(http.MethodGet, "/notfound", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.Code)
	}
	if res.Header().Get("Content-Encoding") == "" {
		t.Fatalf("expected Content-Encoding for 404 body above threshold")
	}
}

func TestCompressResponse_UnsupportedEncodingIgnored(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "hello"})
	})

	// Only unsupported encodings — should pass through uncompressed.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "lzma, bz2")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding for unsupported, got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestCompressResponse_EmptyResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/empty", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/empty", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", res.Code)
	}
	if res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding for empty response")
	}
}

func TestCompressResponse_WriteString(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		_, _ = c.Writer.WriteString(largePayloadString(10))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestCompressResponse_AllUncompressibleTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	uncompressible := []string{
		"image/png",
		"image/jpeg",
		"video/mp4",
		"application/zip",
		"application/gzip",
		"application/pdf",
		"audio/mpeg",
	}

	for _, ct := range uncompressible {
		router := gin.New()
		router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
		router.GET("/test", func(c *gin.Context) {
			c.Data(http.StatusOK, ct, largePayload(20))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Header().Get("Content-Encoding") != "" {
			t.Fatalf("content type %s: expected no Content-Encoding, got %s", ct, res.Header().Get("Content-Encoding"))
		}
	}
}

func TestCompressResponse_CompressibleTypes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	compressible := []string{
		"text/plain",
		"text/html",
		"application/json",
		"application/javascript",
		"text/css",
	}

	for _, ct := range compressible {
		router := gin.New()
		router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
		router.GET("/test", func(c *gin.Context) {
			c.Data(http.StatusOK, ct, largePayload(5))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)

		if res.Header().Get("Content-Encoding") != "gzip" {
			t.Fatalf("content type %s: expected Content-Encoding: gzip, got %s", ct, res.Header().Get("Content-Encoding"))
		}
	}
}

func TestCompressResponse_AsteriskQZero(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
	})

	// *;q=0 means "no encoding accepted".
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "*;q=0")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected no Content-Encoding for *;q=0, got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestCompressResponse_MultipleEncodingsSameQ(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"data": largePayload(5)})
	})

	// Same quality — should prefer zstd based on internal priority.
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0.5, br;q=0.5, zstd;q=0.5")
	res := httptest.NewRecorder()
	router.ServeHTTP(res, req)

	if res.Header().Get("Content-Encoding") != "zstd" {
		t.Fatalf("expected Content-Encoding: zstd (highest internal priority), got %s", res.Header().Get("Content-Encoding"))
	}
}

func TestNegotiateEncoding_Empty(t *testing.T) {
	if enc := negotiateEncoding(""); enc != "" {
		t.Fatalf("expected empty string for empty header, got %s", enc)
	}
}

func TestNegotiateEncoding_OnlyUnsupported(t *testing.T) {
	if enc := negotiateEncoding("lzma, bz2"); enc != "" {
		t.Fatalf("expected empty for unsupported, got %s", enc)
	}
}

func TestIsCompressibleContentType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"", true},
		{"application/json", true},
		{"text/plain", true},
		{"text/html; charset=utf-8", true},
		{"image/png", false},
		{"image/jpeg", false},
		{"application/zip", false},
		{"application/gzip", false},
		{"video/mp4", false},
		{"aPpLiCaTiOn/jSoN", true},
	}

	for _, tc := range tests {
		result := isCompressibleContentType(tc.contentType)
		if result != tc.expected {
			t.Fatalf("isCompressibleContentType(%q) = %v, want %v", tc.contentType, result, tc.expected)
		}
	}
}

// ---- Benchmarks ----

func BenchmarkCompressResponse_Gzip(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := largePayload(100) // ~3KB

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", payload)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
	}
}

func BenchmarkCompressResponse_Zstd(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := largePayload(100)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", payload)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "zstd")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
	}
}

func BenchmarkCompressResponse_Brotli(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := largePayload(100)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", payload)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "br")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
	}
}

func BenchmarkCompressResponse_NoCompression(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := largePayload(100)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", payload)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		// No Accept-Encoding header
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
	}
}

func BenchmarkCompressResponse_SmallPayload(b *testing.B) {
	gin.SetMode(gin.TestMode)
	payload := []byte(`{"message":"short"}`)

	router := gin.New()
	router.Use(CompressResponse(CompressConfig{MinCompressBytes: 1024}))
	router.GET("/test", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json", payload)
	})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Encoding", "gzip")
		res := httptest.NewRecorder()
		router.ServeHTTP(res, req)
	}
}

// min returns the smaller of a and b.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func largePayloadString(size int) string {
	return strings.Repeat("This is a test payload that repeats to create a compressible string. ", size)
}

// ---- CompressibleContentType tests ----

func TestCompressibleContentType(t *testing.T) {
	if !CompressibleContentType("application/json") {
		t.Fatal("expected compressible")
	}
	if CompressibleContentType("image/png") {
		t.Fatal("expected not compressible")
	}
}
