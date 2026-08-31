package handlers

import (
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"

	"stellarbill-backend/internal/featureflags"
)

var mu sync.Mutex

func Register(r *gin.Engine) {
	h := NewFeatureFlagsHandler(featureflags.GetInstance())
	a := r.Group("/api/admin")
	a.Use(func(c *gin.Context) {
		p, _ := c.Get("permissions")
		list, _ := p.([]string)
		for _, x := range list {
			if x == "manage:subscriptions" {
				c.Next()
				return
			}
		}
		c.AbortWithStatus(http.StatusForbidden)
	})
	a.GET("/feature-flags", h.GetFeatureFlags)
	a.PATCH("/feature-flags", func(c *gin.Context) {
		if c.GetHeader("Idempotency-Key") == "" {
			c.AbortWithStatus(http.StatusBadRequest)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		h.ToggleFeatureFlag(c)
	})
}
