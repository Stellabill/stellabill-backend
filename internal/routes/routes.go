package routes

import (
	"log"
	"os"
	"time"

	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/idempotency"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/startup"
	"stellarbill-backend/internal/tracing"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/reconciliation"

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

	r.Use(middleware.CORS(cfg.Env, cfg.AllowedOrigins))

	// Apply rate limiting middleware
	rateLimitConfig := middleware.RateLimiterConfig{
		Enabled:        cfg.RateLimitEnabled,
		Mode:           middleware.RateLimitMode(cfg.RateLimitMode),
		RequestsPerSec: int64(cfg.RateLimitRPS),
		BurstSize:      int64(cfg.RateLimitBurst),
		WhitelistPaths: cfg.RateLimitWhitelist,
	}
	r.Use(middleware.RateLimitMiddleware(rateLimitConfig))

	store := idempotency.NewStore(idempotency.DefaultTTL)
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-secret"
	}

	// Each cached repo gets its own InMemory cache instance so that Flush is
	// scoped to its namespace and does not evict entries from other caches.
	planCache := cache.NewInMemory()
	subCache := cache.NewInMemory()
	const repoCacheTTL = 5 * time.Minute

	rawPlanRepo := repository.NewMockPlanRepo()
	rawSubRepo := repository.NewMockSubscriptionRepo()

	cachedPlanRepo := repository.NewCachedPlanRepo(rawPlanRepo, planCache, repoCacheTTL)
	cachedSubRepo := repository.NewCachedSubscriptionRepo(rawSubRepo, subCache, repoCacheTTL)

	svc := service.NewSubscriptionService(cachedSubRepo, cachedPlanRepo)

	// Statement service wiring (in-memory mock for test/dev)
	stmtRepo := repository.NewMockStatementRepo()
	stmtSvc := service.NewStatementService(rawSubRepo, stmtRepo)

	// Admin handler receives the cached repos so PurgeCache can invalidate them.
	adminToken := os.Getenv("ADMIN_TOKEN")
	adminHandler := handlers.NewAdminHandler(adminToken, cachedPlanRepo, cachedSubRepo)
	// Wire the cached plan repo into the package-level ListPlans handler.
	handlers.SetPlanRepository(cachedPlanRepo)

	// Define the API version/group
	api := r.Group("/api")
	v1 := api.Group("/v1")

	dep := middleware.DeprecationHeaders()

	api.Use(idempotency.Middleware(store))
	v1.Use(middleware.AuthMiddleware(jwtSecret))
	{
		// Public health check - no authentication required
		api.GET("/health", dep, handlers.Health)
		v1.GET("/health", handlers.Health)

		// Public read (user + admin)
		api.GET("/plans",
			dep,
			auth.RequirePermission(auth.PermReadPlans),
			handlers.ListPlans,
		)

		api.GET("/subscriptions",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			handlers.ListSubscriptions,
		)

		api.GET("/subscriptions/:id",
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
