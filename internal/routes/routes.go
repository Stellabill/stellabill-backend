package routes

import (
	"fmt"
	"os"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/cors"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/idempotency"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/startup"
	"stellarbill-backend/internal/tracing"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/security"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func Register(r *gin.Engine) {
	cfg, err := config.Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load configuration: %v", err))
	}

	// Initialize tracing
	if cfg.TracingExporter != "none" {
		_, err := tracing.InitTracer(cfg.TracingServiceName)
		if err != nil {
			// Log error but continue
			log.Printf("Failed to initialize tracer: %v", err)
		}
	}

	// Hardened panic recovery is registered at the engine level so it covers
	// every middleware and handler that follows. RequestID is installed
	// first so panics that fire before downstream middleware still produce a
	// response with a correlation id. Both are no-ops if the parent main()
	// already attached them — Gin will just record duplicate handlers.
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery())

	// Add OpenTelemetry middleware
	r.Use(otelgin.Middleware(cfg.TracingServiceName))
	// Add TraceID middleware to bridge OTEL trace ID to response headers
	r.Use(middleware.TraceIDMiddleware())

	corsProfile := cors.ProfileForEnv(cfg.Env, os.Getenv("ALLOWED_ORIGINS"))

	// Apply rate limiting middleware with per-route overrides for sensitive endpoints
	rateLimitConfig := middleware.RateLimiterConfig{
		Enabled:        cfg.RateLimitEnabled,
		Mode:           middleware.RateLimitMode(cfg.RateLimitMode),
		RequestsPerSec: int64(cfg.RateLimitRPS),
		BurstSize:      int64(cfg.RateLimitBurst),
		WhitelistPaths: cfg.RateLimitWhitelist,
		LogRateLimitHits: true, // Enable logging for security monitoring
		RouteConfigs: map[string]middleware.RouteSpecificConfig{
			// Stricter limits for expensive list endpoints
			"/api/plans":        {RequestsPerSec: 5, BurstSize: 10},
			"/api/subscriptions": {RequestsPerSec: 5, BurstSize: 10},
			// Even stricter for reconciliation endpoint (admin-only, high-cost operation)
			"/api/admin/reconcile": {RequestsPerSec: 2, BurstSize: 5},
		},
	}
	r.Use(middleware.RateLimitMiddleware(rateLimitConfig))

	r.Use(cors.Middleware(corsProfile))

	// Request size limit - enforced BEFORE body parsing to prevent memory abuse
	// Global default from config, per-route overrides via inline middleware
	r.Use(middleware.RequestSizeLimit(cfg.MaxRequestSize))

	// Gzip policy - accept only gzip, reject decompression bombs
	r.Use(middleware.GzipPolicy(middleware.GzipPolicyConfig{
		MaxUncompressedBytes: cfg.MaxGzipUncompressed,
		MaxRatio:             cfg.MaxGzipRatio,
	}))

	store := idempotency.NewStore(idempotency.DefaultTTL)
	jwtSecret := cfg.JWTSecret

	subRepo := repository.NewMockSubscriptionRepo()
	planRepo := repository.NewMockPlanRepo()

	// Statement service wiring (in-memory mock for test/dev)
	stmtRepo := repository.NewMockStatementRepo()
	stmtSvc := service.NewStatementService(subRepo, stmtRepo)

	// Admin handler (token from env or default)
	adminHandler := handlers.NewAdminHandler(cfg.AdminToken)
	// wire planRepo into handlers for list/detail endpoints and optional caching
	handlers.SetPlanRepository(planRepo)

	// Define the API version/group
	api := r.Group("/api")
	v1 := api.Group("/v1")

	dep := middleware.DeprecationHeaders()

	api.Use(idempotency.Middleware(store))
	api.Use(middleware.MaintenanceMode())
	v1.Use(middleware.AuthMiddleware(jwtSecret))
	v1.Use(idempotency.Middleware(store))
	{
		// Public health check - no authentication required
		api.GET("/health", dep, handlers.Health)

		// Versioned API endpoints (v1) with authentication
		// Public read (user + admin) - moved to v1 for consistency
		v1.GET("/plans",
			dep,
			auth.RequirePermission(auth.PermReadPlans),
			handlers.ListPlans,
		)

		v1.GET("/subscriptions",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			handlers.ListSubscriptions,
		)

		v1.GET("/subscriptions/:id",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			handlers.GetSubscription,
		)

		// Example future admin-only endpoints:
		// api.POST("/plans", auth.RequirePermission(auth.PermManagePlans), ...)
		api.GET("/subscriptions", dep, handlers.ListSubscriptions)
		v1.GET("/subscriptions", handlers.ListSubscriptions)
		api.GET("/subscriptions/:id", dep, middleware.AuthMiddleware(jwtSecret), handlers.NewGetSubscriptionHandler(svc))
		v1.GET("/subscriptions/:id", middleware.AuthMiddleware(jwtSecret), handlers.NewGetSubscriptionHandler(svc))
		api.GET("/plans", dep, handlers.ListPlans)
		v1.GET("/plans", handlers.ListPlans)

		api.GET("/statements/:id", middleware.AuthMiddleware(jwtSecret), handlers.NewGetStatementHandler(stmtSvc))
		api.GET("/statements", middleware.AuthMiddleware(jwtSecret), handlers.NewListStatementsHandler(stmtSvc))

		admin := api.Group("/admin")
		{
			admin.POST("/maintenance/enable", auth.RequirePermission(auth.PermManageSubscriptions), handlers.EnableMaintenance)
			admin.POST("/maintenance/disable", auth.RequirePermission(auth.PermManageSubscriptions), handlers.DisableMaintenance)
			admin.POST("/purge", adminHandler.PurgeCache)
			// Diagnostics endpoint — re-runs startup checks for live triage
			diagHandler := startup.NewDiagnosticsHandler(cfg, nil, nil)
			admin.GET("/diagnostics", auth.RequirePermission(auth.PermManageSubscriptions), diagHandler.Handle)
			// Reconciliation endpoint (admin-only) - accepts backend subscription list
			// Choose adapter implementation via env var CONTRACT_SNAPSHOT_URL. If set, use HTTPAdapter.
			contractURL := os.Getenv("CONTRACT_SNAPSHOT_URL")
			var adapter reconciliation.Adapter
			if contractURL != "" {
				// Optional auth header via CONTRACT_SNAPSHOT_AUTH (e.g. "Bearer <token>")
				authHeader := os.Getenv("CONTRACT_SNAPSHOT_AUTH")
				adapter = reconciliation.NewHTTPAdapter(contractURL, authHeader)
			} else {
				// Default to in-memory adapter (empty) — replace or seed as needed in dev.
				adapter = reconciliation.NewMemoryAdapter()
			}
			// Wire in-memory store for persistence by default; can be swapped for DB-backed store.
			reconStore := reconciliation.NewMemoryStore()
			admin.POST("/reconcile", auth.RequirePermission(auth.PermManageSubscriptions), handlers.NewReconcileHandler(adapter, reconStore))
			// List persisted reports
			admin.GET("/reports", auth.RequirePermission(auth.PermManageSubscriptions), func(c *gin.Context) {
				reports, err := reconStore.ListReports()
				if err != nil {
					c.JSON(500, gin.H{"error": "failed to load reports"})
					return
				}
				c.JSON(200, gin.H{"reports": reports})
			})
		}
	}
}
