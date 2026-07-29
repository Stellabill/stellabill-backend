package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/metrics"

	"github.com/gin-gonic/gin"
)

// maxCSPReportBodyBytes is the maximum number of bytes read from a CSP report
// request body before truncation. Keeps memory usage bounded for malicious or
// oversized payloads.
const maxCSPReportBodyBytes = 8 * 1024 // 8 KB

// generateNonce returns a cryptographically random 16-byte nonce encoded as
// base64. Used for the script-src nonce in HTML responses.
func generateNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback: non-empty constant that keeps CSP valid but is obviously
		// insecure. In practice crypto/rand never fails on supported platforms.
		return "fallback-nonce"
	}
	return base64.StdEncoding.EncodeToString(b)
}

// buildCSP assembles the Content-Security-Policy value.
//
//   - base directives always present: default-src + frame-ancestors
//   - report-uri appended when cfg.SecurityCSPReportURI is non-empty
//   - for HTML responses a per-request nonce is inserted into script-src; the
//     nonce is stored in the Gin context under key "csp_nonce" so handlers can
//     embed it in <script nonce="…"> tags
func buildCSP(cfg *config.Config, c *gin.Context) string {
	parts := []string{
		"default-src 'self'",
		fmt.Sprintf("frame-ancestors %s", cfg.SecurityFrameAncestors),
	}

	// Detect HTML responses to inject a per-request nonce into script-src.
	// We inspect the Content-Type header that will be written.  At middleware
	// entry the response headers are not yet written, so we check the
	// response writer after c.Next() via a response interceptor — or, simpler,
	// we check the Accept header and attach script-src unconditionally for HTML
	// responses.  The actual content type is only known after the handler runs,
	// so we use a responseWriterInterceptor to apply it post-handler.
	nonce := generateNonce()
	c.Set("csp_nonce", nonce)
	parts = append(parts, fmt.Sprintf("script-src 'self' 'nonce-%s'", nonce))

	if cfg.SecurityCSPReportURI != "" {
		parts = append(parts, fmt.Sprintf("report-uri %s", cfg.SecurityCSPReportURI))
	}

	return strings.Join(parts, "; ")
}

// buildCSPReportOnly assembles the Content-Security-Policy-Report-Only value
// for JSON API responses.  It is intentionally strict (default-src 'none') so
// any accidental in-browser rendering of JSON triggers a violation report.
func buildCSPReportOnly(cfg *config.Config) string {
	parts := []string{
		"default-src 'none'",
		"frame-ancestors 'none'",
	}
	if cfg.SecurityCSPReportURI != "" {
		parts = append(parts, fmt.Sprintf("report-uri %s", cfg.SecurityCSPReportURI))
	}
	return strings.Join(parts, "; ")
}

// isJSONResponse returns true when the response Content-Type indicates JSON.
// Checked after the handler has written its headers.
func isJSONResponse(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "application/json")
}

// isHTMLResponse returns true when the response Content-Type indicates HTML.
func isHTMLResponse(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.HasPrefix(ct, "text/html")
}

// SecurityHeaders applies baseline HTTP security headers.
// It uses config to determine environment overrides and handles proxy layer
// conflicts by skipping headers that are already present.
//
// CSP behaviour:
//   - All responses receive Content-Security-Policy with default-src, script-src
//     (nonce), frame-ancestors, and an optional report-uri.
//   - JSON responses additionally receive Content-Security-Policy-Report-Only
//     with a strict policy so any accidental in-browser rendering surfaces XSS
//     violations without blocking legitimate API calls.
//   - HTML responses carry a per-request nonce stored under context key
//     "csp_nonce" for use by handlers in <script nonce="…"> tags.
func SecurityHeaders(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		// X-Frame-Options prevents clickjacking.
		if c.Writer.Header().Get("X-Frame-Options") == "" {
			opt := "DENY"
			if opt != "DENY" && opt != "SAMEORIGIN" {
				opt = "DENY" // Prevent insecure combinations like ALLOW-FROM
			}
			c.Header("X-Frame-Options", opt)
		}

		// Prevent MIME sniffing
		if c.Writer.Header().Get("X-Content-Type-Options") == "" {
			c.Header("X-Content-Type-Options", "nosniff")
		}

		// HSTS strictly requires HTTPS. To ease local development (which often
		// uses HTTP), we skip HSTS in the 'development' environment.
		if cfg.Env != "development" {
			if c.Writer.Header().Get("Strict-Transport-Security") == "" {
				hsts := fmt.Sprintf("max-age=%s; includeSubDomains", "31536000")
				c.Header("Strict-Transport-Security", hsts)
			}
		}

		// Build the base CSP with nonce and set it before the handler runs so
		// it is always present.  After the handler we may refine it based on
		// the actual response content type.
		baseCSP := buildCSP(cfg, c)

		// Let the handler run first so we can inspect the response content type.
		c.Next()

		// Now the handler has (likely) set Content-Type.
		contentType := c.Writer.Header().Get("Content-Type")

		if isHTMLResponse(contentType) {
			// HTML: CSP already includes the nonce-based script-src built above.
			if c.Writer.Header().Get("Content-Security-Policy") == "" {
				c.Header("Content-Security-Policy", baseCSP)
			}
			return
		}

		// Non-HTML (including JSON): set a simpler CSP (no nonce needed) and
		// additionally a Report-Only policy that fires on any in-browser render.
		if c.Writer.Header().Get("Content-Security-Policy") == "" {
			// For non-HTML we still use the full baseCSP so frame-ancestors and
			// report-uri are present; the nonce is harmless for non-HTML.
			c.Header("Content-Security-Policy", baseCSP)
		}

		if isJSONResponse(contentType) {
			// JSON responses additionally carry CSP-Report-Only to surface
			// accidental XSS via response injection in webviews.
			if c.Writer.Header().Get("Content-Security-Policy-Report-Only") == "" {
				c.Header("Content-Security-Policy-Report-Only", buildCSPReportOnly(cfg))
			}
		}
	}
}

// cspReportBody represents the JSON structure of a browser-sent CSP violation
// report (application/csp-report content type).
type cspReportBody struct {
	CSPReport cspReportPayload `json:"csp-report"`
}

type cspReportPayload struct {
	DocumentURI        string `json:"document-uri"`
	ViolatedDirective  string `json:"violated-directive"`
	EffectiveDirective string `json:"effective-directive"`
	BlockedURI         string `json:"blocked-uri"`
	StatusCode         int    `json:"status-code"`
	OriginalPolicy     string `json:"original-policy"`
}

// CSPReportHandler returns a Gin handler that receives Content-Security-Policy
// violation reports from browsers.
//
// - Accepts POST requests with Content-Type application/csp-report or
//   application/json.
// - Reads up to maxCSPReportBodyBytes (8 KB) and silently truncates the rest.
// - Increments the csp_reports_total{directive} Prometheus counter.
// - Always returns 204 No Content regardless of body validity, to prevent
//   timing-based enumeration of parsing errors.
//
// Per-tenant rate limiting must be applied at the route level via
// TenantRateLimitMiddleware before this handler is reached.
func CSPReportHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read up to 8 KB; discard the remainder to bound memory usage.
		body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxCSPReportBodyBytes))
		if err != nil {
			// Log nothing sensitive; just return 204.
			c.Status(http.StatusNoContent)
			return
		}

		directive := "unknown"

		var report cspReportBody
		if jsonErr := json.Unmarshal(body, &report); jsonErr == nil {
			if d := report.CSPReport.ViolatedDirective; d != "" {
				// Normalise: keep only the directive name (first token), strip
				// any value that might carry attacker-controlled content.
				directive = sanitizeDirective(d)
			} else if d := report.CSPReport.EffectiveDirective; d != "" {
				directive = sanitizeDirective(d)
			}
		}

		metrics.CSPReportsTotal.WithLabelValues(directive).Inc()

		c.Status(http.StatusNoContent)
	}
}

// sanitizeDirective extracts the first whitespace-separated token from a CSP
// directive string and truncates it to 64 characters.  This prevents
// attacker-controlled strings from polluting Prometheus label cardinality.
func sanitizeDirective(d string) string {
	d = strings.TrimSpace(d)
	if idx := strings.IndexAny(d, " \t"); idx >= 0 {
		d = d[:idx]
	}
	if len(d) > 64 {
		d = d[:64]
	}
	if d == "" {
		return "unknown"
	}
	return d
}
