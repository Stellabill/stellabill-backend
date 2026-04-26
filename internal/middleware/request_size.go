package middleware

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

func RequestSizeLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if maxBytes <= 0 {
			c.Next()
			return
		}

		bodyLen, err := io.ReadAll(io.LimitReader(c.Request.Body, maxBytes+1))
		if err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{
				"error": "bad_request",
			})
			return
		}

		if int64(len(bodyLen)) > maxBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{
				"error":     "request_too_large",
				"max_bytes": maxBytes,
			})
			return
		}

		c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyLen))
		c.Next()
	}
}
