package handlers

import (
	"stellarbill-backend/internal/pagination"

	"github.com/gin-gonic/gin"
)

// requestBaseURL reconstructs the absolute URL the client used to reach this
// handler (scheme + host + path + existing query string), so pagination Link
// headers can round-trip through the same route and filters.
//
// Scheme detection: a TLS-terminated connection reports "https" directly.
// For requests proxied over plain HTTP to this service (e.g. behind a
// TLS-terminating load balancer) we fall back to X-Forwarded-Proto. That
// header is only used to choose "http" vs "https" for a Link header value —
// never to authorize, route, or make trust decisions — so a spoofed value at
// worst produces a link with the wrong scheme, not a security issue.
func requestBaseURL(c *gin.Context) string {
	if c.Request == nil || c.Request.URL == nil {
		return ""
	}

	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	} else if proto := c.GetHeader("X-Forwarded-Proto"); proto == "https" || proto == "http" {
		scheme = proto
	}

	// Build from Path/RawQuery explicitly rather than c.Request.URL.String():
	// an absolute-form request target (e.g. relayed through a proxy, or as
	// constructed by some test helpers) can leave Scheme/Host set on
	// c.Request.URL itself, and String() would then render a second
	// scheme://host prefix ahead of the one we're adding here.
	path := c.Request.URL.Path
	if rq := c.Request.URL.RawQuery; rq != "" {
		path += "?" + rq
	}
	return scheme + "://" + c.Request.Host + path
}

// setPaginationLinkHeader builds and sets the RFC 5988 Link header for a
// cursor-paginated list endpoint from a pagination.Page. hasPrev must be true
// whenever the current request supplied a non-empty incoming cursor (i.e.
// this is not the first page) — see pagination.LinkParams.HasPrev.
func setPaginationLinkHeader[T any](c *gin.Context, page pagination.Page[T], hasPrev bool) {
	header := pagination.LinkHeader(requestBaseURL(c), pagination.LinkParams{
		Next:    page.NextCursor,
		Prev:    page.PreviousCursor,
		HasPrev: hasPrev,
	})
	if header != "" {
		c.Header("Link", header)
	}
}
