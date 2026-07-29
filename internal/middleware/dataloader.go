package middleware

import (
	"stellarbill-backend/internal/repository"

	"github.com/gin-gonic/gin"
)

const DataLoaderKey = "loader"

// DataLoaderMiddleware attaches a new request-scoped repository.Loader to the Gin context and request context.
func DataLoaderMiddleware(planRepo repository.PlanRepository, subRepo repository.SubscriptionRepository, opts ...repository.Option) gin.HandlerFunc {
	return func(c *gin.Context) {
		loader := repository.NewLoader(planRepo, subRepo, opts...)

		// Store in Gin context and std context
		c.Set(DataLoaderKey, loader)
		c.Request = c.Request.WithContext(repository.WithLoader(c.Request.Context(), loader))

		c.Next()
	}
}

// GetDataLoader extracts the Loader from Gin context if available.
func GetDataLoader(c *gin.Context) *repository.Loader {
	if val, exists := c.Get(DataLoaderKey); exists {
		if l, ok := val.(*repository.Loader); ok {
			return l
		}
	}
	// Fallback to std context
	return repository.LoaderFromContext(c.Request.Context())
}
