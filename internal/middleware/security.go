package middleware

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
)

// SecurityHeaders applies baseline HTTP security headers.
// It uses config to determine environment overrides and handles proxy layer conflicts
// by passing conditionally if headers aren't already written.
func SecurityHeaders(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// X-Frame-Options prevents clickjacking.
		if c.Writer.Header().Get("X-Frame-Options") == "" {
			opt := cfg.SecurityFrameOpt
			if opt != "DENY" && opt != "SAMEORIGIN" {
				opt = "DENY" // Prevent insecure combinations like ALLOW-FROM
			}
			c.Header("X-Frame-Options", opt)
		}

		// Prevent MIME sniffing
		if c.Writer.Header().Get("X-Content-Type-Options") == "" {
			c.Header("X-Content-Type-Options", "nosniff")
		}

		// HSTS strictly requires HTTPS. To ease local development (which often uses HTTP),
		// we skip HSTS in the 'development' environment.
		if cfg.Env != "development" {
			if c.Writer.Header().Get("Strict-Transport-Security") == "" {
				hsts := fmt.Sprintf("max-age=%s; includeSubDomains", cfg.SecurityHSTSMaxAge)
				c.Header("Strict-Transport-Security", hsts)
			}
		}

		// Content-Security-Policy: frame-ancestors
		if c.Writer.Header().Get("Content-Security-Policy") == "" {
			csp := fmt.Sprintf("frame-ancestors %s", cfg.SecurityFrameAncestors)
			c.Header("Content-Security-Policy", csp)
		}

		c.Next()
	}
}
