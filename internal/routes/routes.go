package routes

import (
	"fmt"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/startup"
	"stellarbill-backend/internal/tracing"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Register configures all routes on the provided router.
func Register(r *gin.Engine) {
	cfg, err := config.Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load configuration: %v", err))
	}

	// Initialize tracing
	if cfg.TracingExporter != "none" {
		_, err := tracing.InitTracer(cfg.TracingServiceName)
		if err != nil {
			fmt.Printf("Failed to initialize tracer: %v\n", err)
		}
	}

	// Global middleware
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery())
	r.Use(otelgin.Middleware(cfg.TracingServiceName))
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

	// Configure per-tenant rate limiting middleware
	tenantRateLimitConfig := middleware.TenantRateLimitConfig{
		Enabled:          cfg.RateLimitEnabled,
		RPS:              cfg.RateLimitTenantRPS,
		Burst:            cfg.RateLimitTenantBurst,
		LogRateLimitHits: false,
	}

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

	// API Groups
	api := r.Group("/api")
	v1 := api.Group("/v1")

	dep := middleware.DeprecationHeaders()

	// Public health check
	api.GET("/health", dep, h.LivenessProbe)
	v1.GET("/health", h.LivenessProbe)
	api.GET("/liveness", h.LivenessProbe)
	api.GET("/readiness", h.ReadinessProbe)

	// V1 routes are all protected
	v1.Use(authMiddleware)
	v1.Use(middleware.TenantRateLimitMiddleware(tenantRateLimitConfig))
	{
		v1.GET("/subscriptions", h.ListSubscriptions)
		v1.GET("/subscriptions/:id", handlers.NewGetSubscriptionHandler(svc))
		v1.POST("/subscriptions/:id/status", auth.RequirePermission(auth.PermManageSubscriptions), handlers.NewChangeSubscriptionStatusHandler(svc))
		v1.GET("/plans", h.ListPlans)
		v1.GET("/statements/:id", handlers.NewGetStatementHandler(stmtSvc))
		v1.GET("/statements", handlers.NewListStatementsHandler(stmtSvc))
	}

	// Legacy /api routes - also protected
	apiProtected := api.Group("")
	apiProtected.Use(authMiddleware)
	apiProtected.Use(middleware.TenantRateLimitMiddleware(tenantRateLimitConfig))
	{
		apiProtected.GET("/plans",
			dep,
			auth.RequirePermission(auth.PermReadPlans),
			h.ListPlans,
		)

		apiProtected.GET("/subscriptions",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			h.ListSubscriptions,
		)

		apiProtected.GET("/subscriptions/:id",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			h.GetSubscription,
		)
		apiProtected.POST("/subscriptions/:id/status",
			dep,
			auth.RequirePermission(auth.PermManageSubscriptions),
			handlers.NewChangeSubscriptionStatusHandler(svc),
		)

		apiProtected.GET("/statements/:id", handlers.NewGetStatementHandler(stmtSvc))
		apiProtected.GET("/statements", handlers.NewListStatementsHandler(stmtSvc))
	}

	admin := api.Group("/admin")
	admin.Use(authMiddleware)
	admin.Use(middleware.TenantRateLimitMiddleware(tenantRateLimitConfig))
	{
		admin.POST("/purge", adminHandler.PurgeCache)
		// Diagnostics endpoint — re-runs startup checks for live triage
		diagHandler := startup.NewDiagnosticsHandler(cfg, nil, nil)
		admin.GET("/diagnostics", auth.RequirePermission(auth.PermManageSubscriptions), diagHandler.Handle)

		// Reconciliation — scoped by RBAC and tenant
		adapter := reconciliation.NewMemoryAdapter()
		reconStore := reconciliation.NewMemoryStore()
		admin.POST("/reconcile", auth.RequirePermission(auth.PermManageSubscriptions), handlers.NewReconcileHandler(adapter, reconStore))
		admin.GET("/reports", auth.RequirePermission(auth.PermReadReconciliation), handlers.NewListReportsHandler(reconStore))
	}
}
