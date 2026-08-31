package audit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
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
	actor := ResolveActor(c)

	// Convert map[string]string to map[string]interface{}
	auditMeta := make(map[string]interface{})
	for k, v := range meta {
		auditMeta[k] = v
	}

	_, _ = logger.Log(c.Request.Context(), AuditEvent{
		Actor:    actor,
		Action:   action,
		Resource: target,
		Outcome:  outcome,
		Metadata: auditMeta,
	})
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
	meta := map[string]interface{}{
		"path":        c.FullPath(),
		"method":      c.Request.Method,
		"status":      strconv.Itoa(status),
		"auth_header": c.GetHeader("Authorization"),
	}
	if reason != "" {
		meta["reason"] = reason
	}
	actor := ResolveActor(c)

	_, _ = logger.Log(c.Request.Context(), AuditEvent{
		Actor:    actor,
		Action:   "auth_failure",
		Resource: c.FullPath(),
		Outcome:  "failure",
		Metadata: meta,
	})
}

// Middleware returns a Gin middleware that logs HTTP requests as audit events.
func Middleware(logger *Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		duration := time.Since(start)

		actor := ResolveActor(c)
		meta := map[string]interface{}{
			"path":       c.FullPath(),
			"method":     c.Request.Method,
			"status":     strconv.Itoa(c.Writer.Status()),
			"duration":   duration.String(),
			"client_ip":  c.ClientIP(),
			"user_agent": c.Request.UserAgent(),
		}

		outcome := "success"
		if c.Writer.Status() >= 400 {
			outcome = "failure"
		}

		_, _ = logger.Log(c.Request.Context(), AuditEvent{
			Actor:    actor,
			Action:   "http_request",
			Resource: c.FullPath(),
			Outcome:  outcome,
			Metadata: meta,
		})
	}
}

// AuditEvent represents an audit log entry.
type AuditEvent struct {
	Actor    string                 `json:"actor"`
	Action   string                 `json:"action"`
	Resource string                 `json:"resource"`
	Outcome  string                 `json:"outcome"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// Logger defines the interface for audit logging.
type Logger interface {
	Log(ctx context.Context, event AuditEvent) (string, error)
	LastHash() string
}
