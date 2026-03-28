package ingestion

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// NewIngestHandler returns a Gin handler that accepts a single contract event
// for ingestion. It validates the request, delegates to the Service, and
// returns the normalised event or an appropriate error.
func NewIngestHandler(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var raw RawEvent
		if err := c.ShouldBindJSON(&raw); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
			return
		}

		event, err := svc.Consume(c.Request.Context(), raw)
		if err != nil {
			switch err {
			case ErrDuplicateEvent:
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case ErrOutOfOrder:
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case ErrMissingIdempotencyKey, ErrMissingEventType, ErrInvalidEventType,
				ErrMissingContractID, ErrMissingTenantID, ErrMissingOccurredAt,
				ErrInvalidOccurredAt, ErrFutureOccurredAt, ErrInvalidPayload,
				ErrNegativeSequence:
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			}
			return
		}

		c.JSON(http.StatusCreated, gin.H{
			"data": event,
		})
	}
}

// NewListByContractHandler returns a Gin handler that lists normalised
// contract events for a given contract_id.
func NewListByContractHandler(repo EventRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		contractID := c.Param("contract_id")
		if contractID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "contract_id required"})
			return
		}

		limit := 50
		if l := c.Query("limit"); l != "" {
			if v, err := strconv.Atoi(l); err == nil && v > 0 && v <= 200 {
				limit = v
			}
		}
		offset := 0
		if o := c.Query("offset"); o != "" {
			if v, err := strconv.Atoi(o); err == nil && v >= 0 {
				offset = v
			}
		}

		events, err := repo.ListByContractID(c.Request.Context(), contractID, limit, offset)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if events == nil {
			events = []*ContractEvent{}
		}

		c.JSON(http.StatusOK, gin.H{
			"data":   events,
			"limit":  limit,
			"offset": offset,
		})
	}
}
