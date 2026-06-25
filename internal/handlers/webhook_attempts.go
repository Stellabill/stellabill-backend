package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"stellarbill-backend/internal/outbox"
)

// NewWebhookAttemptsHandler returns GET /api/v1/webhooks/:id/attempts.
//
// Security: tenantID is taken from the verified JWT context (set by AuthMiddleware).
// Cross-tenant access is prevented because ListAttempts filters by both eventID and
// tenantID — a caller for tenant-A will receive an empty list for an event that
// belongs to tenant-B, with no information leak.
func NewWebhookAttemptsHandler(repo outbox.AttemptRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		tenantID, ok := c.Get("tenantID")
		if !ok || tenantID == "" {
			RespondWithError(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "missing tenant context")
			return
		}
		tid, ok := tenantID.(string)
		if !ok || tid == "" {
			RespondWithError(c, http.StatusUnauthorized, ErrorCodeUnauthorized, "invalid tenant context")
			return
		}

		eventID, err := uuid.Parse(c.Param("id"))
		if err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid webhook id")
			return
		}

		attempts, err := repo.ListAttempts(tid, eventID)
		if err != nil {
			RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternalError, "failed to retrieve attempts")
			return
		}

		// Return an empty array (not null) when there are no attempts.
		if attempts == nil {
			attempts = []*outbox.Attempt{}
		}

		c.JSON(http.StatusOK, gin.H{"attempts": attempts})
	}
}
