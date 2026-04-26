package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type GzipPolicyConfig struct {
	MaxUncompressedBytes int64
	MaxRatio             float64
}

func GzipPolicy(cfg GzipPolicyConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		encoding := c.GetHeader("Content-Encoding")
		encoding = strings.TrimSpace(strings.ToLower(encoding))

		if encoding == "" || encoding == "identity" {
			c.Next()
			return
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
				"error":                "decompression_bomb",
				"decompressed_size":    decompressed.Len(),
				"max_uncompressed":     maxDestSize,
				"compressed_size":      compressedLen,
				"compression_ratio":   float64(decompressed.Len()) / float64(max(1, int(compressedLen))),
			})
			return
		}

		c.Request.Body = io.NopCloser(&decompressed)
		c.Request.Header.Del("Content-Encoding")
		c.Next()
	}
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}