package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/cache"
)

// purgeResponse is the JSON payload returned by the admin purge endpoint.
type purgeResponse struct {
	Status          string           `json:"status"`
	TotalKeysPurged int              `json:"total_keys_purged"`
	Namespaces      []purgeNamespace `json:"namespaces,omitempty"`
	Timestamp       time.Time        `json:"timestamp"`
	Message         string           `json:"message,omitempty"`
}

type purgeNamespace struct {
	Namespace     string `json:"namespace"`
	KeysPurged    int    `json:"keys_purged"`
	CountersReset bool   `json:"counters_reset"`
	Error         string `json:"error,omitempty"`
}

type namespaceSummary struct {
	Namespace     string `json:"namespace"`
	KeysPurged    int    `json:"keys_purged"`
	CountersReset bool   `json:"counters_reset"`
	Error         string `json:"error,omitempty"`
}

// AdminHandler encapsulates admin-only HTTP operations.
type AdminHandler struct {
	expectedToken string
	purgeables    []cache.Purgeable
}

// NewAdminHandler constructs an AdminHandler with the provided token and optional purgeables.
func NewAdminHandler(token string, purgeables ...cache.Purgeable) *AdminHandler {
	return &AdminHandler{expectedToken: token, purgeables: purgeables}
}

// PurgeCache handles cache purge requests. It is a lightweight implementation
// that supports the test expectations for auth and cache purging.
func (h *AdminHandler) PurgeCache(c *gin.Context) {
	if token := c.GetHeader("X-Admin-Token"); token == "" || token != h.expectedToken {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var namespaces []purgeNamespace
	totalKeysPurged := 0
	status := "purged"
	for _, purgeable := range h.purgeables {
		if purgeable == nil {
			continue
		}
		keysPurged, err := purgeable.Flush(context.Background())
		if err != nil {
			status = "partial"
			namespaces = append(namespaces, purgeNamespace{Namespace: purgeable.Namespace(), KeysPurged: 0, CountersReset: false, Error: err.Error()})
			purgeable.ResetMetrics()
			continue
		}
		totalKeysPurged += keysPurged
		purgeable.ResetMetrics()
		namespaces = append(namespaces, purgeNamespace{Namespace: purgeable.Namespace(), KeysPurged: keysPurged, CountersReset: true})
	}

	code := http.StatusOK
	if status == "partial" {
		code = http.StatusAccepted
	}
	c.JSON(code, purgeResponse{
		Status:          status,
		TotalKeysPurged: totalKeysPurged,
		Namespaces:      namespaces,
		Timestamp:       time.Now().UTC(),
	})
}
