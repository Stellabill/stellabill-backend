package middleware

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/brotli"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
)

const (
	MinCompressBytes = 256
)

var skipCompressPrefixes = []string{
	"image/",
	"video/",
	"audio/",
	"font/",
	"application/zip",
	"application/gzip",
	"application/x-gzip",
	"application/zstd",
	"application/x-brotli",
	"application/pdf",
}

type GzipPolicyConfig struct {
	MaxUncompressedBytes int64
	MaxRatio             float64
	ResponseCompression  bool
	MinCompressBytes     int
}

func GzipPolicy(cfg GzipPolicyConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		encoding := c.GetHeader("Content-Encoding")
		encoding = strings.TrimSpace(strings.ToLower(encoding))

		if encoding == "" || encoding == "identity" {
			goto responseCompress
		}

		if encoding != "gzip" {
			c.AbortWithStatusJSON(http.StatusNotAcceptable, gin.H{
				"error":    "unsupported_encoding",
				"encoding": encoding,
			})
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "bad_request",
			})
			return
		}

		compressedLen := int64(len(body))

		if cfg.MaxUncompressedBytes > 0 && compressedLen > cfg.MaxUncompressedBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":           "request_too_large",
				"compressed_size": compressedLen,
				"max_compressed":  cfg.MaxUncompressedBytes,
			})
			return
		}

		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "invalid_gzip",
			})
			return
		}

		var decompressed bytes.Buffer
		maxDestSize := cfg.MaxUncompressedBytes

		if cfg.MaxRatio > 0 && compressedLen > 0 {
			ratioLimit := int64(float64(compressedLen) * cfg.MaxRatio)
			if maxDestSize == 0 || ratioLimit < maxDestSize {
				maxDestSize = ratioLimit
			}
		}

		if maxDestSize > 0 {
			limitedReader := io.LimitReader(zr, maxDestSize+1)
			_, err = io.Copy(&decompressed, limitedReader)
			if err != nil && err != io.EOF {
			}
		} else {
			_, err = io.Copy(&decompressed, zr)
			if err != nil && err != io.EOF {
			}
		}

		if zr.Close() != nil {
		}

		if maxDestSize > 0 && int64(decompressed.Len()) > maxDestSize {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":             "decompression_bomb",
				"decompressed_size": decompressed.Len(),
				"max_uncompressed":  maxDestSize,
				"compressed_size":   compressedLen,
				"compression_ratio": float64(decompressed.Len()) / float64(max(1, int(compressedLen))),
			})
			return
		}

		c.Request.Body = io.NopCloser(&decompressed)
		c.Request.Header.Del("Content-Encoding")

	responseCompress:
		if cfg.ResponseCompression && !c.IsAborted() {
			enc := negotiateEncoding(c)
			if enc != "" {
				minB := cfg.MinCompressBytes
				if minB <= 0 {
					minB = MinCompressBytes
				}
				cw := &compressingWriter{
					ResponseWriter: c.Writer,
					encoding:       enc,
					minCompress:    minB,
					statusCode:     http.StatusOK,
				}
				c.Writer = cw
				defer cw.finalize()
			}
		}

		c.Next()
	}
}

func negotiateEncoding(c *gin.Context) string {
	ae := c.GetHeader("Accept-Encoding")
	if ae == "" {
		return ""
	}
	aeLower := strings.ToLower(ae)
	if strings.Contains(aeLower, "zstd") {
		return "zstd"
	}
	if strings.Contains(aeLower, "br") {
		return "br"
	}
	if strings.Contains(aeLower, "gzip") {
		return "gzip"
	}
	return ""
}

type compressingWriter struct {
	gin.ResponseWriter
	encoding    string
	minCompress int
	buf         bytes.Buffer
	compWriter  io.WriteCloser
	statusCode  int
	wroteHeader bool
	committed   bool
}

func (w *compressingWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.statusCode = code
}

func (w *compressingWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.committed {
		if w.compWriter != nil {
			return w.compWriter.Write(data)
		}
		return w.ResponseWriter.Write(data)
	}
	w.buf.Write(data)
	if w.buf.Len() >= w.minCompress && !shouldSkipCompression(w) {
		w.commitCompressed()
	}
	return len(data), nil
}

func (w *compressingWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *compressingWriter) commitUncompressed() {
	w.ResponseWriter.WriteHeader(w.statusCode)
	if w.buf.Len() > 0 {
		w.ResponseWriter.Write(w.buf.Bytes())
		w.buf.Reset()
	}
	w.committed = true
}

func (w *compressingWriter) commitCompressed() {
	w.ResponseWriter.Header().Del("Content-Length")
	w.ResponseWriter.Header().Set("Content-Encoding", w.encoding)
	w.ResponseWriter.WriteHeader(w.statusCode)

	var err error
	switch w.encoding {
	case "zstd":
		w.compWriter, err = zstd.NewWriter(w.ResponseWriter, zstd.WithEncoderConcurrency(1))
	case "br":
		w.compWriter = brotli.NewWriter(w.ResponseWriter)
	case "gzip":
		w.compWriter = gzip.NewWriter(w.ResponseWriter)
	}
	if err != nil {
		w.ResponseWriter.Header().Del("Content-Encoding")
		w.commitUncompressed()
		return
	}
	if w.buf.Len() > 0 {
		w.compWriter.Write(w.buf.Bytes())
		w.buf.Reset()
	}
	w.committed = true
}

func (w *compressingWriter) finalize() {
	if w.committed {
		if w.compWriter != nil {
			w.compWriter.Close()
		}
		return
	}
	if shouldSkipCompression(w) || w.buf.Len() < w.minCompress {
		w.commitUncompressed()
		return
	}
	w.commitCompressed()
	if w.compWriter != nil {
		w.compWriter.Close()
	}
}

func shouldSkipCompression(w *compressingWriter) bool {
	if w.ResponseWriter.Header().Get("Content-Encoding") != "" {
		return true
	}
	ct := w.ResponseWriter.Header().Get("Content-Type")
	for _, prefix := range skipCompressPrefixes {
		if strings.HasPrefix(ct, prefix) {
			return true
		}
	}
	return false
}
