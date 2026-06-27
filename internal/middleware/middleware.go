package middleware

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// DeprecationHeaders adds Deprecation, Sunset, and Link headers indicating the
// /api/v1 successor route for legacy /api endpoints.
func DeprecationHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Deprecation", "true")
		c.Header("Sunset", time.Now().Add(180*24*time.Hour).Format(time.RFC1123))

		path := c.Request.URL.Path
		const prefix = "/api"
		if strings.HasPrefix(path, prefix) {
			successor := prefix + "/v1" + path[len(prefix):]
			c.Header("Link", `<`+successor+`>; rel="successor-version"`)
		}

		c.Next()
	}
}
