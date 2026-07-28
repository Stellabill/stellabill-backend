package handlers

import (
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/middleware"
)

// IdempotencyInspectResponse is the JSON shape returned by GET /api/v1/idempotency/:key.
type IdempotencyInspectResponse struct {
	// Key is the idempotency key that was queried.
	Key string `json:"key"`
	// UsedAt is when the key was first recorded (RFC 3339).
	UsedAt time.Time `json:"used_at"`
	// ExpiresAt is when the key will be purged (RFC 3339).
	ExpiresAt time.Time `json:"expires_at"`
	// StatusCode is the HTTP status stored with the completed response.
	// 0 indicates the original request is still in-flight.
	StatusCode int `json:"status_code"`
	// RequestFingerprint is the SHA-256 hex digest of the original request body.
	RequestFingerprint string `json:"request_fingerprint"`
}

// IdempotencyHandler handles idempotency-key inspection requests.
type IdempotencyHandler struct {
	store middleware.IdempotencyStore
}

// NewIdempotencyHandler creates an IdempotencyHandler backed by the given store.
func NewIdempotencyHandler(store middleware.IdempotencyStore) *IdempotencyHandler {
	return &IdempotencyHandler{store: store}
}

// InspectKey godoc
//
//	GET /api/v1/idempotency/:key
//
// Returns metadata for a stored idempotency key scoped to the authenticated
// caller's tenant. The caller can only inspect keys they originally created;
// the scope is derived from the same tenantID/callerID context values that the
// Idempotency middleware uses when recording keys.
func (h *IdempotencyHandler) InspectKey(c *gin.Context) {
	key := c.Param("key")
	if key == "" || len(key) > 255 {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "key must be between 1 and 255 characters")
		return
	}

	scope := resolveScope(c)

	rec, err := h.store.Lookup(c.Request.Context(), scope, key)
	if err != nil {
		RespondWithInternalError(c, "failed to look up idempotency key")
		return
	}
	if rec == nil {
		RespondWithNotFoundError(c, fmt.Sprintf("idempotency key %q", key))
		return
	}

	c.JSON(http.StatusOK, IdempotencyInspectResponse{
		Key:                key,
		UsedAt:             rec.UsedAt.UTC(),
		ExpiresAt:          rec.ExpiresAt.UTC(),
		StatusCode:         rec.StatusCode,
		RequestFingerprint: rec.PayloadHash,
	})
}

// resolveScope reconstructs the tenant-scoped key prefix in the same way the
// Idempotency middleware does so callers only see their own keys.
func resolveScope(c *gin.Context) string {
	tenantID, _ := c.Get("tenantID")
	callerID, _ := c.Get("callerID")

	switch {
	case tenantID != nil && callerID != nil:
		return fmt.Sprintf("%v:%v", tenantID, callerID)
	case tenantID != nil:
		return fmt.Sprintf("%v:anonymous", tenantID)
	case callerID != nil:
		return fmt.Sprintf("anonymous:%v", callerID)
	default:
		return "anonymous"
	}
}
