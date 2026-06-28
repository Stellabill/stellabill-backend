package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/outbox"
)

// SubscriberKeysHandler manages subscriber JWK registration for outbox encryption.
type SubscriberKeysHandler struct {
	repo outbox.SubscriberKeyRepository
}

// NewSubscriberKeysHandler creates a subscriber key admin handler.
func NewSubscriberKeysHandler(repo outbox.SubscriberKeyRepository) *SubscriberKeysHandler {
	return &SubscriberKeysHandler{repo: repo}
}

type registerSubscriberKeyRequest struct {
	SubscriberID string          `json:"subscriber_id" binding:"required"`
	KeyID        string          `json:"key_id" binding:"required"`
	JWK          json.RawMessage `json:"jwk" binding:"required"`
	ExpiresAt    *time.Time      `json:"expires_at,omitempty"`
}

// RegisterSubscriberKey handles POST /api/admin/subscriber-keys
func (h *SubscriberKeysHandler) RegisterSubscriberKey(c *gin.Context) {
	if h.repo == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "subscriber key repository not available")
		return
	}

	var req registerSubscriberKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid request body")
		return
	}

	key := &outbox.SubscriberKey{
		SubscriberID: req.SubscriberID,
		KeyID:        req.KeyID,
		JWK:          req.JWK,
		Status:       outbox.SubscriberKeyActive,
		ExpiresAt:    req.ExpiresAt,
	}

	if err := h.repo.Create(key); err != nil {
		RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternalError, "failed to register subscriber key")
		return
	}

	audit.LogAction(c, "subscriber_key_register", key.SubscriberID, "success", map[string]string{
		"key_id": key.KeyID,
	})

	c.JSON(http.StatusCreated, key)
}

// ListSubscriberKeys handles GET /api/admin/subscriber-keys/:subscriber_id
func (h *SubscriberKeysHandler) ListSubscriberKeys(c *gin.Context) {
	if h.repo == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "subscriber key repository not available")
		return
	}

	subscriberID := c.Param("subscriber_id")
	keys, err := h.repo.ListBySubscriber(subscriberID)
	if err != nil {
		RespondWithError(c, http.StatusInternalServerError, ErrorCodeInternalError, "failed to list subscriber keys")
		return
	}

	c.JSON(http.StatusOK, keys)
}

type updateSubscriberKeyRequest struct {
	Status string `json:"status" binding:"required"`
}

// UpdateSubscriberKey handles PATCH /api/admin/subscriber-keys/id/:id
func (h *SubscriberKeysHandler) UpdateSubscriberKey(c *gin.Context) {
	if h.repo == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "subscriber key repository not available")
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid key ID")
		return
	}

	var req updateSubscriberKeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid request body")
		return
	}

	status := outbox.SubscriberKeyStatus(req.Status)
	switch status {
	case outbox.SubscriberKeyActive, outbox.SubscriberKeyRevoked, outbox.SubscriberKeyExpired:
	default:
		RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid status")
		return
	}

	if err := h.repo.UpdateStatus(id, status); err != nil {
		RespondWithError(c, http.StatusNotFound, ErrorCodeNotFound, "subscriber key not found")
		return
	}

	audit.LogAction(c, "subscriber_key_update", id.String(), "success", map[string]string{
		"status": req.Status,
	})

	c.Status(http.StatusNoContent)
}

// GetSubscriberKey handles GET /api/admin/subscriber-keys/id/:id
func (h *SubscriberKeysHandler) GetSubscriberKey(c *gin.Context) {
	if h.repo == nil {
		RespondWithError(c, http.StatusServiceUnavailable, ErrorCodeServiceUnavailable, "subscriber key repository not available")
		return
	}

	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondWithError(c, http.StatusBadRequest, ErrorCodeBadRequest, "invalid key ID")
		return
	}

	key, err := h.repo.GetByID(id)
	if err != nil {
		RespondWithError(c, http.StatusNotFound, ErrorCodeNotFound, "subscriber key not found")
		return
	}

	c.JSON(http.StatusOK, key)
}
