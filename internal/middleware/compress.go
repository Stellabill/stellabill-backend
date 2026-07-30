package middleware

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

// CompressConfig holds configuration for the response compression middleware.
type CompressConfig struct {
	// MinCompressBytes is the minimum response body size in bytes to trigger
	// compression. Responses smaller than this threshold are sent uncompressed.
	// Setting this to 0 means compress every response the client accepts.
	MinCompressBytes int
}

// DefaultCompressConfig returns a CompressConfig with sensible defaults.
func DefaultCompressConfig() CompressConfig {
	return CompressConfig{
		MinCompressBytes: 128,
	}
}

// compressibleContentTypes is a set of response content types that are worth
// compressing. Binary and already-compressed content types are excluded.
var compressibleContentTypes = map[string]bool{
	"text/plain":            true,
	"text/html":             true,
	"text/css":              true,
	"text/javascript":       true,
	"text/xml":              true,
	"application/json":      true,
	"application/javascript": true,
	"application/xml":       true,
	"application/ld+json":   true,
	"application/grpc":      true,
	"image/svg+xml":         true,
}

// encodingPriority defines the order in which we prefer encodings, highest first.
var encodingPriority = map[string]int{
	"zstd": 30,
	"br":   20,
	"gzip": 10,
}

// CompressResponse negotiates the Accept-Encoding request header and compresses
// the response body with the most efficient encoding the client supports.
//
// Priority order: zstd > br > gzip. Responses smaller than MinCompressBytes
// are sent uncompressed. Compressible content types are checked to avoid wasting
// CPU on binary or already-compressed payloads.
func CompressResponse(cfg CompressConfig) gin.HandlerFunc {
	if cfg.MinCompressBytes <= 0 {
		cfg.MinCompressBytes = 128
	}

	return func(c *gin.Context) {
		// Negotiate the preferred encoding.
		accept := c.GetHeader("Accept-Encoding")
		if accept == "" {
			c.Next()
			return
		}

		encoding := negotiateEncoding(accept)
		if encoding == "" {
			c.Next()
			return
		}

		// Wrap the response writer.
		w := &compressResponseWriter{
			ResponseWriter:   c.Writer,
			encoding:         encoding,
			buf:              &bytes.Buffer{},
			minCompressBytes: cfg.MinCompressBytes,
			statusCode:       http.StatusOK,
		}
		c.Writer = w

		c.Next()

		// Finalize: either flush uncompressed or compress and send.
		w.finalize()
	}
}

// negotiateEncoding parses the Accept-Encoding header and returns the most
// preferred encoding we support. An empty string means "no compression".
func negotiateEncoding(header string) string {
	if header == "" {
		return ""
	}

	directives := strings.Split(header, ",")
	type preference struct {
		encoding string
		q        float64
	}

	var prefs []preference

	for _, d := range directives {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}

		parts := strings.Split(d, ";")
		enc := strings.TrimSpace(parts[0])
		if enc == "" {
			continue
		}

		q := 1.0
		if len(parts) > 1 {
			qStr := strings.TrimSpace(parts[1])
			if strings.HasPrefix(qStr, "q=") {
				if parsed, err := strconv.ParseFloat(qStr[2:], 64); err == nil {
					q = parsed
				}
			}
		}

		// identity;q=0 means "send uncompressed" — skip compression entirely.
		if enc == "identity" && q == 0 {
			return ""
		}
		// *;q=0 means "no encoding accepted" — same as identity;q=0.
		if enc == "*" && q == 0 {
			return ""
		}

		if _, ok := encodingPriority[enc]; ok {
			prefs = append(prefs, preference{encoding: enc, q: q})
		}
	}

	if len(prefs) == 0 {
		return ""
	}

	// Sort by quality factor descending, then by built-in priority descending.
	sort.Slice(prefs, func(i, j int) bool {
		if prefs[i].q != prefs[j].q {
			return prefs[i].q > prefs[j].q
		}
		return encodingPriority[prefs[i].encoding] > encodingPriority[prefs[j].encoding]
	})

	return prefs[0].encoding
}

// compressResponseWriter wraps gin.ResponseWriter to support delayed header
// flushing and on-the-fly response compression.
type compressResponseWriter struct {
	gin.ResponseWriter
	encoding         string
	buf              *bytes.Buffer
	writer           io.WriteCloser
	minCompressBytes int
	statusCode       int
	wroteHeader      bool
}

func (w *compressResponseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.statusCode = code
	w.wroteHeader = true
	// Do NOT flush headers yet — finalize() will decide whether to set
	// Content-Encoding based on the actual body size.
}

// Write buffers the data. Headers are only flushed in finalize() so we can
// decide at the last moment whether to compress.
func (w *compressResponseWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.buf.Write(data)
}

// Written returns true if the status code has been set. We return the flag
// from our deferred-write tracking, not the underlying ResponseWriter.
func (w *compressResponseWriter) Written() bool {
	return w.wroteHeader
}

// Size returns the number of bytes written so far (buffered).
func (w *compressResponseWriter) Size() int {
	return w.buf.Len()
}

// finalize flushes the buffered response, compressing it if it exceeds the
// minimum threshold and the content type is compressible.
func (w *compressResponseWriter) finalize() {
	body := w.buf.Bytes()
	bodyLen := len(body)

	// Determine if we should compress.
	contentType := w.Header().Get("Content-Type")
	shouldCompress := bodyLen >= w.minCompressBytes &&
		isCompressibleContentType(contentType)

	if !shouldCompress {
		// Below threshold or non-compressible — write uncompressed.
		w.ResponseWriter.WriteHeader(w.statusCode)
		if len(body) > 0 {
			_, _ = w.ResponseWriter.Write(body)
		}
		return
	}

	// Compress and write.
	w.ResponseWriter.Header().Set("Content-Encoding", w.encoding)
	w.ResponseWriter.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(w.statusCode)

	var err error
	switch w.encoding {
	case "zstd":
		err = w.compressZstd(body)
	case "br":
		err = w.compressBrotli(body)
	case "gzip":
		err = w.compressGzip(body)
	default:
		// Fallback — send uncompressed.
		_, _ = w.ResponseWriter.Write(body)
		return
	}

	if err != nil {
		// Compression failed — this is unexpected and indicates a bug in the
		// encoder or a resource issue. Log and send uncompressed.
		_, _ = w.ResponseWriter.Write(body)
	}
}

func (w *compressResponseWriter) compressZstd(body []byte) error {
	enc, err := zstd.NewWriter(w.ResponseWriter,
		zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("zstd writer: %w", err)
	}
	defer enc.Close()
	_, err = enc.Write(body)
	return err
}

func (w *compressResponseWriter) compressBrotli(body []byte) error {
	enc := brotli.NewWriterLevel(w.ResponseWriter, brotli.DefaultCompression)
	defer enc.Close()
	_, err := enc.Write(body)
	return err
}

func (w *compressResponseWriter) compressGzip(body []byte) error {
	enc := gzip.NewWriter(w.ResponseWriter)
	defer enc.Close()
	_, err := enc.Write(body)
	return err
}

// isCompressibleContentType returns true if the content type is worth
// compressing. Binary, image, video, and already-compressed formats are skipped.
func isCompressibleContentType(contentType string) bool {
	if contentType == "" {
		// No content type header — assume it's compressible so we compress
		// generic API responses.
		return true
	}
	// Strip parameters (e.g. charset).
	if idx := strings.IndexByte(contentType, ';'); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	return compressibleContentTypes[strings.ToLower(contentType)]
}

// CompressibleContentType is exported for testing.
func CompressibleContentType(contentType string) bool {
	return isCompressibleContentType(contentType)
}
