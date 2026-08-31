package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/outbox"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// defaultDeadLetterLimit and maxDeadLetterLimit bound the number of
// dead-lettered events returned by a single GET request. Listing is an
// operational/administrative action; capping the page size prevents an
// unbounded scan and accidental memory pressure.
const (
	defaultDeadLetterLimit = 100
	maxDeadLetterLimit     = 200
)

// errRequeueNonFailed is matched against the repository error returned when an
// event is not found or is not in the failed (dead-letter) status. The string
// is kept in sync with internal/outbox repository implementations; the
// repository currently returns it as a plain error (not a sentinel).
const errRequeueNonFailed = "event not found or not in failed status"

// OutboxAdminHandler exposes administrative inspection and recovery for outbox
// events. It requires an outbox.Repository; when the repository is unavailable
// the handlers respond 503 so callers can distinguish an unhealthy backend
// from an invalid request.
type OutboxAdminHandler struct {
	repo outbox.Repository
}

// NewOutboxAdminHandler builds an OutboxAdminHandler. A nil repository is
// allowed (handlers return 503) so route wiring can degrade gracefully when
// the database is not configured.
func NewOutboxAdminHandler(repo outbox.Repository) *OutboxAdminHandler {
	return &OutboxAdminHandler{repo: repo}
}

// ListDeadLetteredEvents handles GET /api/admin/outbox/dead-letter.
// It returns the events currently in the dead letter (failed) projection,
// newest first, bounded by the optional `limit` query parameter.
func (h *OutboxAdminHandler) ListDeadLetteredEvents(c *gin.Context) {
	if h.repo == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "outbox repository not available")
		return
	}

	limit := defaultDeadLetterLimit
	if raw := c.Query("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			RespondWithValidationError(c, "limit must be a positive integer", map[string]interface{}{
				"reason": "invalid limit",
				"limit":  raw,
			})
			return
		}
		limit = parsed
		if limit > maxDeadLetterLimit {
			limit = maxDeadLetterLimit
		}
	}

	events, err := h.repo.ListDeadLetteredEvents(limit)
	if err != nil {
		RespondWithInternalError(c, "failed to list dead-lettered outbox events")
		return
	}

	c.JSON(http.StatusOK, gin.H{"events": events})
}

// RequeueOutboxEvent handles POST /api/admin/outbox/:id/requeue.
// It resets a failed (dead-lettered) event to pending with its retry counters
// cleared so the outbox worker retries it. The operation is idempotent for
// repeated calls with the same event: once reset, a second requeue reports 404
// ("not in failed status") which is the expected, safe outcome.
func (h *OutboxAdminHandler) RequeueOutboxEvent(c *gin.Context) {
	if h.repo == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "outbox repository not available")
		return
	}

	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		RespondWithValidationError(c, "invalid outbox event id", map[string]interface{}{
			"reason": "id must be a UUID",
			"id":     idStr,
		})
		return
	}

	if err := h.repo.RequeueEvent(id); err != nil {
		if isRequeueNonFailed(err) {
			RespondWithNotFoundError(c, "outbox event")
			return
		}
		RespondWithInternalError(c, "failed to requeue outbox event")
		return
	}

	// Emit an audit event so operators have a trail of manual recovery actions.
	// Audit logging failures are non-fatal: the requeue itself has already been
	// committed by the repository.
	audit.LogAction(c, "outbox.requeue", idStr, "success", map[string]string{
		"event_id": idStr,
	})

	c.Status(http.StatusNoContent)
}

// isRequeueNonFailed reports whether err indicates the targeted event is
// missing or not in the failed (dead-letter) status.
func isRequeueNonFailed(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, errRequeueNonFailed) || strings.Contains(err.Error(), errRequeueNonFailed)
}
