package routes

import (
	"os"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func Register(r *gin.Engine) {
	// Global middleware - order matters!
	r.Use(middleware.Recovery())
	r.Use(middleware.RequestID())
	r.Use(corsMiddleware())

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-secret"
	}

	subRepo := repository.NewMockSubscriptionRepo()
	planRepo := repository.NewMockPlanRepo()
	svc := service.NewSubscriptionService(subRepo, planRepo)

	api := r.Group("/api")
	{
		api.GET("/health", handlers.Health)
		api.GET("/outbox/stats", handlers.OutboxStats)
		api.POST("/outbox/test", handlers.PublishTestEvent)
		api.GET("/subscriptions", handlers.ListSubscriptions)
		api.GET("/subscriptions/:id", middleware.AuthMiddleware(jwtSecret), handlers.NewGetSubscriptionHandler(svc))
		api.GET("/plans", handlers.ListPlans)
		
		// Test endpoints for panic recovery (only in non-production)
		api.GET("/test/panic", handlers.TestPanicHandler)
		api.GET("/test/panic-after-write", handlers.PanicAfterWriteHandler)
		api.GET("/test/nested-panic", handlers.NestedPanicHandler)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
