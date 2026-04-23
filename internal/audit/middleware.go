package audit

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/requestid"
)

const loggerContextKey = "_audit_logger"

// Middleware attaches the audit logger to the request context and records auth failures.
func Middleware(logger *Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		if logger != nil {
			c.Set(loggerContextKey, logger)
		}
		c.Next()

		status := c.Writer.Status()
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			logAuthFailure(c, logger, status)
		}
	}
}

// LogAction is a helper for handlers to record admin/sensitive activity.
func LogAction(c *gin.Context, action, target, outcome string, metadata map[string]string) {
	raw, ok := c.Get(loggerContextKey)
	if !ok {
		return
	}
	logger, ok := raw.(*Logger)
	if !ok || logger == nil {
		return
	}
	meta := ensureMetadata(metadata)
	meta["path"] = c.FullPath()
	meta["method"] = c.Request.Method
	meta["client_ip"] = c.ClientIP()
	// Add request_id from Gin context if present
	if rid, ok := c.Get(requestid.ContextKey); ok {
		if ridStr, ok := rid.(string); ok && ridStr != "" {
			meta[requestid.ContextKey] = ridStr
		}
	}
	actor := ResolveActor(c)
	logger.Log(c.Request.Context(), actor, action, target, outcome, meta)
}

// ResolveActor attempts to infer the actor from headers or previously-set values.
func ResolveActor(c *gin.Context) string {
	if c == nil {
		return "anonymous"
	}
	if v, ok := c.Get("actor"); ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	if h := c.GetHeader("X-Actor"); strings.TrimSpace(h) != "" {
		return strings.TrimSpace(h)
	}
	if h := c.GetHeader("X-User"); strings.TrimSpace(h) != "" {
		return strings.TrimSpace(h)
	}
	return c.ClientIP()
}

func logAuthFailure(c *gin.Context, logger *Logger, status int) {
	if logger == nil {
		return
	}
	reason := ""
	if len(c.Errors) > 0 {
		reason = c.Errors[0].Error()
	}
	meta := map[string]string{
		"path":        c.FullPath(),
		"method":      c.Request.Method,
		"status":      strconv.Itoa(status),
		"auth_header": c.GetHeader("Authorization"),
	}
	if reason != "" {
		meta["reason"] = reason
	}
	actor := ResolveActor(c)
	logger.Log(c.Request.Context(), actor, "auth_failure", c.FullPath(), fmt.Sprintf("status_%d", status), meta)
}

func ensureMetadata(meta map[string]string) map[string]string {
	if meta == nil {
		return map[string]string{}
	}
	return meta
}
