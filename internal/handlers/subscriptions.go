package handlers

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/pagination"
	"stellarbill-backend/internal/service"
)
// SSE for Issue #357: Server-Sent Events for live subscription status
// - Fan-out hub + heartbeats every 15s
// - Graceful shutdown on context done
// - Ready for outbox dispatcher integration
type Subscription struct {
	ID          string    `json:"id"`
	PlanID      string    `json:"plan_id"`
	Customer    string    `json:"customer"`
	Status      string    `json:"status"`
	Amount      string    `json:"amount"`
	Interval    string    `json:"interval"`
	NextBilling string    `json:"next_billing,omitempty"`
	UpdatedAt   time.Time `json:"-"`
	Version     int64     `json:"-"`
	ETag        string    `json:"etag"`
}

func (s Subscription) GetID() string        { return s.ID }
func (s Subscription) GetSortValue() string { return s.Customer } // Sort by customer for now

func (h *Handler) ListSubscriptions(c *gin.Context) {
	limitStr := c.DefaultQuery("limit", "10")
	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 10
	}

	cursorStr := c.Query("cursor")
	cursor, err := pagination.Decode(cursorStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid cursor format"})
		return
	}

	allSubs, err := h.Subscriptions.ListSubscriptions(c)
	if err != nil {
		RespondWithInternalError(c, "Failed to retrieve subscriptions")
		return
	}

	page := pagination.PaginateSlice(allSubs, cursor, limit)

	c.JSON(http.StatusOK, gin.H{
		"subscriptions": page.Items,
		"next_cursor":   page.NextCursor,
		"has_more":      page.HasMore,
	})
}

func (h *Handler) GetSubscription(c *gin.Context) {
	id := c.Param("id")
	sub, err := h.Subscriptions.GetSubscription(c, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	
	etag := GenerateETag(sub.UpdatedAt, sub.Version)
	c.Header("ETag", etag)
	
	c.JSON(http.StatusOK, sub)
}

func (h *Handler) PatchSubscription(c *gin.Context) {
	id := c.Param("id")
	expectedVersion, err := EnsureIfMatch(c)
	if err != nil {
		return
	}

	var req Subscription
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.Subscriptions.PatchSubscription(c, id, &req, expectedVersion); err != nil {
		if err.Error() == "concurrent update" {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "precondition failed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated"})
}

func (h *Handler) DeleteSubscription(c *gin.Context) {
	id := c.Param("id")
	expectedVersion, err := EnsureIfMatch(c)
	if err != nil {
		return
	}

	if err := h.Subscriptions.DeleteSubscription(c, id, expectedVersion); err != nil {
		if err.Error() == "concurrent update" {
			c.JSON(http.StatusPreconditionFailed, gin.H{"error": "precondition failed"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.Status(http.StatusNoContent)
}

// NewGetSubscriptionHandler returns a gin.HandlerFunc that retrieves a full
// subscription detail using the provided SubscriptionService.
func NewGetSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
	}
}

// NewChangeSubscriptionStatusHandler updates a single subscription status.
func NewChangeSubscriptionStatusHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var payload struct {
			Status string `json:"status"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "invalid request body")
			return
		}

		status := strings.TrimSpace(payload.Status)
		if status == "" {
			RespondWithError(c, http.StatusUnprocessableEntity, ErrorCodeValidationFailed, "status is required")
			return
		}

		tenantID := c.GetString("tenantID")
		if tenantID == "" {
			RespondWithAuthError(c, "missing tenant context")
			return
		}

		change, err := svc.ChangeStatus(c.Request.Context(), tenantID, c.GetString("callerID"), c.Param("id"), status)
		if err != nil {
			statusCode, code, message := MapServiceErrorToResponse(err)
			if errors.Is(err, service.ErrInvalidStatus) || errors.Is(err, service.ErrInvalidTransition) || errors.Is(err, service.ErrUnknownCurrentState) {
				statusCode = http.StatusConflict
				code = ErrorCodeConflict
				if errors.Is(err, service.ErrInvalidStatus) {
					statusCode = http.StatusUnprocessableEntity
					code = ErrorCodeValidationFailed
				}
			}
			RespondWithError(c, statusCode, code, message)
			return
		}

		c.JSON(http.StatusOK, gin.H{"api_version": "v1", "data": change})
	}
}

// NewBatchSubscriptionHandler accepts a batch of subscription status updates and returns
// per-item status codes in a 207 Multi-Status response.
func NewBatchSubscriptionHandler(svc service.SubscriptionService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req service.BatchSubscriptionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, "invalid request body")
			return
		}

		results, err := svc.ProcessBatch(c.Request.Context(), c.GetString("tenantID"), c.GetString("callerID"), req.Operations)
		if err != nil {
			RespondWithError(c, http.StatusBadRequest, ErrorCodeValidationFailed, err.Error())
			return
		}

		response := service.BatchSubscriptionResponse{Results: results}
		statusCode := http.StatusOK
		if len(results) > 0 {
			for _, result := range results {
				if result.StatusCode >= http.StatusBadRequest {
					statusCode = http.StatusMultiStatus
					break
				}
			}
		}
		if statusCode == http.StatusMultiStatus {
			c.JSON(statusCode, response)
			return
		}
		c.JSON(statusCode, response)
	}
}

// SubscriptionEvent represents a status change event for SSE
type SubscriptionEvent struct {
	SubscriptionID string `json:"subscription_id"`
	Status         string `json:"status"`
	Timestamp      string `json:"timestamp"`
	TenantID       string `json:"tenant_id,omitempty"`
}

// SimpleFanOutHub is a basic fan-out hub for SSE (fed by outbox later)
type SimpleFanOutHub struct {
	clients   map[chan SubscriptionEvent]bool
	broadcast chan SubscriptionEvent
}

var hub = &SimpleFanOutHub{
	clients:   make(map[chan SubscriptionEvent]bool),
	broadcast: make(chan SubscriptionEvent, 100),
}

// run starts the hub (called on startup in real impl)
func (h *SimpleFanOutHub) run() {
	for event := range h.broadcast {
		for client := range h.clients {
			select {
			case client <- event:
			default:
				close(client)
				delete(h.clients, client)
			}
		}
	}
}

// GetSubscriptionEvents handles SSE stream for live subscription updates
func (h *Handler) GetSubscriptionEvents(c *gin.Context) {
	// TODO: Extract tenant from auth token (follow patterns in other handlers like reconciliation.go)
	// tenantID := getTenantFromContext(c)

	clientChan := make(chan SubscriptionEvent, 10)

	hub.clients[clientChan] = true
	defer func() {
		delete(hub.clients, clientChan)
		close(clientChan)
	}()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("Access-Control-Allow-Origin", "*")

	c.Stream(func(w io.Writer) bool {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-c.Request.Context().Done():
				return false // graceful shutdown / client disconnect
			case event, ok := <-clientChan:
				if !ok {
					return false
				}
				// Filter by tenant in real impl
				fmt.Fprintf(w, "data: %s\n\n", `{"subscription_id":"`+event.SubscriptionID+`","status":"`+event.Status+`","timestamp":"`+event.Timestamp+`"}`)
				c.Writer.Flush()
			case <-ticker.C:
				fmt.Fprintf(w, ": heartbeat\n\n")
				c.Writer.Flush()
			}
		}
	})
}