package middleware

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	// RegionRoleActive serves writes locally.
	RegionRoleActive = "active"
	// RegionRolePassive forwards or redirects writes to the active region.
	RegionRolePassive = "passive"

	// RegionForwardModeProxy reverse-proxies the write to the active region.
	RegionForwardModeProxy = "proxy"
	// RegionForwardModeRedirect returns HTTP 302 with the active region URL.
	RegionForwardModeRedirect = "redirect"

	// RegionHopHeader prevents write-forward loops across regions.
	RegionHopHeader = "X-Region-Hop"
	// RegionHopValue is the value stamped on forwarded requests.
	RegionHopValue = "1"
)

// RegionRouterConfig configures cross-region write forwarding.
type RegionRouterConfig struct {
	// Role is "active" (default) or "passive".
	Role string
	// ActiveRegionURL is the base URL of the active region (required when Role=passive).
	ActiveRegionURL string
	// ForwardMode is "proxy" (default) or "redirect".
	ForwardMode string
	// ForwardAuthToken is sent as Authorization: Bearer <token> when proxying.
	ForwardAuthToken string
	// HTTPClient is used for proxy forwards; if nil a default client is created.
	HTTPClient *http.Client
	// Timeout bounds the proxy round-trip (default 10s).
	Timeout time.Duration
}

// RegionRouterMiddleware forwards write requests from a passive region to the
// active region. Read methods (GET/HEAD/OPTIONS) always pass through.
//
// Loop prevention: requests that already carry X-Region-Hop: 1 are rejected
// with 508 Loop Detected instead of being forwarded again.
//
// When the active region is unreachable in proxy mode, the middleware returns
// 503 Service Unavailable with a helpful body.
func RegionRouterMiddleware(cfg RegionRouterConfig) gin.HandlerFunc {
	role := strings.ToLower(strings.TrimSpace(cfg.Role))
	if role == "" {
		role = RegionRoleActive
	}
	mode := strings.ToLower(strings.TrimSpace(cfg.ForwardMode))
	if mode == "" {
		mode = RegionForwardModeProxy
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}

	var activeBase *url.URL
	if role == RegionRolePassive && cfg.ActiveRegionURL != "" {
		if u, err := url.Parse(cfg.ActiveRegionURL); err == nil && u.Scheme != "" && u.Host != "" {
			activeBase = u
		}
	}

	tracer := otel.Tracer("stellarbill/middleware/region_router")

	return func(c *gin.Context) {
		if role != RegionRolePassive {
			c.Next()
			return
		}
		if !isWriteMethod(c.Request.Method) {
			c.Next()
			return
		}

		ctx, span := tracer.Start(c.Request.Context(), "region.write_forward",
			trace.WithAttributes(
				attribute.String("region.role", role),
				attribute.String("region.forward_mode", mode),
				attribute.String("http.method", c.Request.Method),
				attribute.String("http.target", c.Request.URL.Path),
			),
		)
		defer span.End()
		c.Request = c.Request.WithContext(ctx)

		// Loop prevention — never forward a request that was already hopped.
		if c.GetHeader(RegionHopHeader) == RegionHopValue {
			span.SetStatus(codes.Error, "region hop loop detected")
			c.AbortWithStatusJSON(http.StatusLoopDetected, gin.H{
				"error":   "region_hop_loop",
				"message": "write was already forwarded once; refusing to forward again",
			})
			return
		}

		if activeBase == nil {
			span.SetStatus(codes.Error, "active region URL not configured")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":   "active_region_unavailable",
				"message": "passive region cannot accept writes: ACTIVE_REGION_URL is not configured",
			})
			return
		}

		target := *activeBase
		target.Path = singleJoinPath(activeBase.Path, c.Request.URL.Path)
		target.RawQuery = c.Request.URL.RawQuery

		span.SetAttributes(
			attribute.Bool("region.forwarded", true),
			attribute.String("region.active_url", target.String()),
		)

		if mode == RegionForwardModeRedirect {
			c.Header(RegionHopHeader, RegionHopValue)
			c.Redirect(http.StatusFound, target.String())
			c.Abort()
			return
		}

		// Proxy mode: reverse-proxy the write to the active region.
		proxyReq, err := http.NewRequestWithContext(ctx, c.Request.Method, target.String(), c.Request.Body)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "failed to build forward request")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":   "active_region_unavailable",
				"message": "failed to build forward request to active region",
			})
			return
		}
		copyHeaders(c.Request.Header, proxyReq.Header)
		proxyReq.Header.Set(RegionHopHeader, RegionHopValue)
		if cfg.ForwardAuthToken != "" {
			proxyReq.Header.Set("Authorization", "Bearer "+cfg.ForwardAuthToken)
		}

		resp, err := client.Do(proxyReq)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "active region unreachable")
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error":   "active_region_unavailable",
				"message": fmt.Sprintf("active region unreachable: %v", err),
			})
			return
		}
		defer resp.Body.Close()

		for k, vals := range resp.Header {
			for _, v := range vals {
				c.Writer.Header().Add(k, v)
			}
		}
		c.Writer.Header().Set(RegionHopHeader, RegionHopValue)
		c.Status(resp.StatusCode)
		_, _ = io.Copy(c.Writer, resp.Body)
		c.Abort()
	}
}

func isWriteMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func singleJoinPath(base, rel string) string {
	base = strings.TrimSuffix(base, "/")
	if !strings.HasPrefix(rel, "/") {
		rel = "/" + rel
	}
	if base == "" {
		return rel
	}
	return base + rel
}

func copyHeaders(src, dst http.Header) {
	for k, vals := range src {
		// Hop-by-hop headers must not be forwarded.
		switch strings.ToLower(k) {
		case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
			"te", "trailers", "transfer-encoding", "upgrade", "content-length":
			continue
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}
