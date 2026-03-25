package routes

import (
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/cors"
	"stellarbill-backend/internal/handlers"
	"os"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/idempotency"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
)

func Register(r *gin.Engine) {
	cfg := config.Load()
	corsProfile := cors.ProfileForEnv(cfg.Env, cfg.AllowedOrigins)

	r.Use(cors.Middleware(corsProfile))

	store := idempotency.NewStore(idempotency.DefaultTTL)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-secret"
	}

	subRepo := repository.NewMockSubscriptionRepo()
	planRepo := repository.NewMockPlanRepo()
	svc := service.NewSubscriptionService(subRepo, planRepo)

	api := r.Group("/api")
	api.Use(idempotency.Middleware(store))
	{
		// Public health check - no authentication required
		api.GET("/health", handlers.Health)

		// Public read (user + admin)
		api.GET("/plans",
			auth.RequirePermission(auth.PermReadPlans),
			handlers.ListPlans,
		)

		api.GET("/subscriptions",
			auth.RequirePermission(auth.PermReadSubscriptions),
			handlers.ListSubscriptions,
		)

		api.GET("/subscriptions/:id",
			auth.RequirePermission(auth.PermReadSubscriptions),
			handlers.GetSubscription,
		)

		// Example future admin-only endpoints:
		// api.POST("/plans", auth.RequirePermission(auth.PermManagePlans), ...)
		api.GET("/subscriptions", handlers.ListSubscriptions)
		api.GET("/subscriptions/:id", middleware.AuthMiddleware(jwtSecret), handlers.NewGetSubscriptionHandler(svc))
		api.GET("/plans", handlers.ListPlans)

		admin := api.Group("/admin")
		{
			admin.POST("/purge", adminHandler.PurgeCache)
		}
	}

	return nil
}
