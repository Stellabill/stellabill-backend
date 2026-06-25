package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
)

const (
	CSPNonceKey = "csp_nonce"
)

type cspReportPayload struct {
	Report map[string]any `json:"csp-report"`
}

// SecurityHeaders applies baseline HTTP security headers.
// It uses config to determine environment overrides and handles proxy layer conflicts
// by passing conditionally if headers aren't already written.
func SecurityHeaders(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		nonce, err := generateCSPNonce()
		if err != nil {
			c.Error(err)
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		c.Set(CSPNonceKey, nonce)

		// X-Frame-Options prevents clickjacking.
		if c.Writer.Header().Get("X-Frame-Options") == "" {
			opt := "DENY"
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
				hsts := fmt.Sprintf("max-age=%s; includeSubDomains", "31536000")
				c.Header("Strict-Transport-Security", hsts)
			}
		}

		if c.Writer.Header().Get("Content-Security-Policy") == "" {
			csp := "frame-ancestors 'none'"
			c.Header("Content-Security-Policy", csp)
		}

		c.Next()
	}
}

func buildCSP(cfg *config.Config, nonce string) string {
	parts := []string{
		"default-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		fmt.Sprintf("frame-ancestors %s", cfg.SecurityFrameAncestors),
		fmt.Sprintf("script-src 'self' 'nonce-%s'", nonce),
		"style-src 'self'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"form-action 'self'",
	}
	if cfg.SecurityCSPReportURI != "" {
		parts = append(parts, fmt.Sprintf("report-uri %s", cfg.SecurityCSPReportURI))
	}
	return strings.Join(parts, "; ")
}

func generateCSPNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(b), nil
}

func GetCSPNonce(c *gin.Context) string {
	val, _ := c.Get(CSPNonceKey)
	if nonce, ok := val.(string); ok {
		return nonce
	}
	return ""
}

// CSPReportHandler accepts browser violation reports and logs them for diagnostics.
func CSPReportHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload cspReportPayload
		if err := c.ShouldBindJSON(&payload); err == nil && len(payload.Report) > 0 {
			log.Printf("CSP violation report: %v", payload.Report)
			c.Status(http.StatusNoContent)
			return
		}

		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Status(http.StatusNoContent)
			return
		}
		if len(body) > 0 {
			log.Printf("CSP violation report: %s", strings.TrimSpace(string(body)))
		}
		c.Status(http.StatusNoContent)
	}
}
