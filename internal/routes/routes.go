package routes

import (
	"fmt"
	"os"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/cors"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/idempotency"
	"stellarbill-backend/internal/logger"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repositories"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/startup"
	"stellarbill-backend/internal/tracing"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/security"

	"log"
	"time"

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
			logger.Log.Errorf("Failed to initialize tracer: %v", err)
		}
	}

	// Hardened panic recovery is registered at the engine level so it covers
	// every middleware and handler that follows. RequestID is installed
	// first so panics that fire before downstream middleware still produce a
	// response with a correlation id. Both are no-ops if the parent main()
	// already attached them — Gin will just record duplicate handlers.
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery(log.Default())) // Pass a default logger

	// Add OpenTelemetry middleware
	r.Use(otelgin.Middleware(cfg.TracingServiceName))
	// Add TraceID middleware to bridge OTEL trace ID to response headers
	r.Use(middleware.TraceIDMiddleware())

	corsProfile := cors.ProfileForEnv(cfg.Env, cfg.AllowedOrigins)

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
	jwksCache := auth.NewJWKSCache(cfg.JWKSURL, 1*time.Hour)

	// Old repository mocks for the service layer
	subRepoOld := repository.NewMockSubscriptionRepo()
	planRepoOld := repository.NewMockPlanRepo()
	stmtRepoOld := repository.NewMockStatementRepo()

	svc := service.NewSubscriptionService(subRepoOld, planRepoOld)
	stmtSvc := service.NewStatementService(subRepoOld, stmtRepoOld)

	// Admin handler (token from env or default)
	adminHandler := handlers.NewAdminHandler(cfg.AdminToken)
	
	// New repositories mocks for the handler layer
	subRepo := repositories.NewMockSubscriptionRepository()
	planRepo := repositories.NewMockPlanRepository()

	// Main handler
	h := handlers.NewHandler(nil, nil, nil, nil)
	h.SubRepo = subRepo
	h.PlanRepo = planRepo

	// Define the API version/group
	api := r.Group("/api")
	fmt.Printf("DEBUG: Registering /api group\n")
	v1 := api.Group("/v1")
	fmt.Printf("DEBUG: Registering /api/v1 group\n")

	dep := middleware.DeprecationHeaders()

	api.Use(idempotency.Middleware(store))
	api.Use(middleware.MaintenanceMode())
	v1.Use(middleware.AuthMiddleware(jwksCache))
	v1.Use(idempotency.Middleware(store))
	{
		// Public health check - no authentication required
		api.GET("/health", dep, h.HealthDetails)

		// Versioned API endpoints (v1) with authentication
		// Public read (user + admin) - moved to v1 for consistency
		v1.GET("/plans",
			dep,
			auth.RequirePermission(auth.PermReadPlans),
			h.ListPlans,
		)

		v1.GET("/subscriptions",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			h.ListSubscriptions,
		)

		v1.GET("/subscriptions/:id",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			handlers.NewGetSubscriptionHandler(svc),
		)

		v1.GET("/statements/:id", middleware.AuthMiddleware(jwksCache), handlers.NewGetStatementHandler(stmtSvc))
		v1.GET("/statements", middleware.AuthMiddleware(jwksCache), handlers.NewListStatementsHandler(stmtSvc))

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
				adapter = reconciliation.NewHTTPAdapter(contractURL, authHeader, security.DevLogger())
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
