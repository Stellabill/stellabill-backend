// Package repository implements data access for the Stellabill API.
package repository

import (
	"github.com/gin-gonic/gin"
)

// DataLoaderKey is the Gin context key that stores the request-scoped Loader.
const DataLoaderKey = "loader"

// DataLoaderMiddleware attaches a new request-scoped Loader to the Gin context
// and request context.
func DataLoaderMiddleware(planRepo PlanRepository, subRepo SubscriptionRepository, opts ...Option) gin.HandlerFunc {
	return func(c *gin.Context) {
		loader := NewLoader(planRepo, subRepo, opts...)

		// Store in Gin context and std context
		c.Set(DataLoaderKey, loader)
		c.Request = c.Request.WithContext(WithLoader(c.Request.Context(), loader))

		c.Next()
	}
}

// GetDataLoader extracts the Loader from Gin context if available.
func GetDataLoader(c *gin.Context) *Loader {
	if val, exists := c.Get(DataLoaderKey); exists {
		if l, ok := val.(*Loader); ok {
			return l
		}
	}
	// Fallback to std context
	return LoaderFromContext(c.Request.Context())
}
