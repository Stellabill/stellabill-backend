package routes

import (
	"os"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/cors"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/idempotency"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/tracing"

	"stellarbill-backend/internal/auth"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func Register(r *gin.Engine) {
	cfg := config.Load()

	// Initialize tracing
	if cfg.TracingExporter != "none" {
		_, err := tracing.InitTracer(cfg.TracingServiceName)
		if err != nil {
			// Log error but continue
			middleware.Log.Errorf("Failed to initialize tracer: %v", err)
		}
	}

	// Add OpenTelemetry middleware
	r.Use(otelgin.Middleware(cfg.TracingServiceName))
	// Add TraceID middleware to bridge OTEL trace ID to response headers
	r.Use(middleware.TraceIDMiddleware())

	corsProfile := cors.ProfileForEnv(cfg.Env, cfg.AllowedOrigins)

	// Apply rate limiting middleware
	rateLimitConfig := middleware.RateLimiterConfig{
		Enabled:        cfg.RateLimitEnabled,
		Mode:           middleware.RateLimitMode(cfg.RateLimitMode),
		RequestsPerSec: int64(cfg.RateLimitRPS),
		BurstSize:      int64(cfg.RateLimitBurst),
		WhitelistPaths: cfg.RateLimitWhitelist,
	}
	r.Use(middleware.RateLimitMiddleware(rateLimitConfig))

	r.Use(cors.Middleware(corsProfile))

	store := idempotency.NewStore(idempotency.DefaultTTL)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-secret"
	}

	subRepo := repository.NewMockSubscriptionRepo()
	planRepo := repository.NewMockPlanRepo()
	svc := service.NewSubscriptionService(subRepo, planRepo)
	// wire planRepo into handlers for list/detail endpoints and optional caching
	handlers.SetPlanRepository(planRepo)

	// Define the API version/group
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

		api.GET("/statements/:id", middleware.AuthMiddleware(jwtSecret), handlers.NewGetStatementHandler(stmtSvc))
		api.GET("/statements", middleware.AuthMiddleware(jwtSecret), handlers.NewListStatementsHandler(stmtSvc))

		admin := api.Group("/admin")
		{
			admin.POST("/purge", adminHandler.PurgeCache)
		}
	}
}
