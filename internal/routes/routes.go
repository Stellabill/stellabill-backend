package routes

import (
	"fmt"
	"net/http"
	"os"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/tracing"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/startup"

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
			fmt.Printf("Failed to initialize tracer: %v\n", err)
		}
	}

	// Global middleware
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery())
	r.Use(otelgin.Middleware(cfg.TracingServiceName))
	r.Use(middleware.TraceIDMiddleware())

	// Rate limiting
	rateLimitConfig := middleware.RateLimiterConfig{
		Enabled:        cfg.RateLimitEnabled,
		Mode:           middleware.RateLimitMode(cfg.RateLimitMode),
		RequestsPerSec: int64(cfg.RateLimitRPS),
		BurstSize:      int64(cfg.RateLimitBurst),
		WhitelistPaths: cfg.RateLimitWhitelist,
	}
	r.Use(middleware.RateLimitMiddleware(rateLimitConfig))

	// Request size and Gzip
	r.Use(middleware.RequestSizeLimit(cfg.MaxRequestSize))
	r.Use(middleware.GzipPolicy(middleware.GzipPolicyConfig{
		MaxUncompressedBytes: cfg.MaxGzipUncompressed,
		MaxRatio:             cfg.MaxGzipRatio,
	}))

	// Dependencies
	subRepo := repository.NewMockSubscriptionRepo()
	_ = repository.NewMockPlanRepo() // Placeholder
	stmtRepo := repository.NewMockStatementRepo()

	stmtSvc := service.NewStatementService(subRepo, stmtRepo)
	svc := service.NewSubscriptionService(subRepo)
	
	// Create handlers
	h := handlers.NewHandler(nil, nil)
	adminHandler := handlers.NewAdminHandler(cfg.AdminToken)
	
	// Auth configuration
	jwtSecret := cfg.JWTSecret
	// For now, jwksCache is nil as it's not fully wired in config yet
	authMiddleware := middleware.AuthMiddleware(nil, jwtSecret)

	// API Groups
	api := r.Group("/api")
	v1 := api.Group("/v1")
	
	dep := middleware.DeprecationHeaders()

	// Public health check
	api.GET("/health", dep, handlers.Health)
	v1.GET("/health", handlers.Health)
	api.GET("/liveness", h.LivenessProbe)
	api.GET("/readiness", h.ReadinessProbe)

	// V1 routes are all protected
	v1.Use(authMiddleware)
	{
		v1.GET("/subscriptions", handlers.ListSubscriptions)
		v1.GET("/subscriptions/:id", handlers.NewGetSubscriptionHandler(svc))
		v1.GET("/plans", handlers.ListPlans)
		v1.GET("/statements/:id", handlers.NewGetStatementHandler(stmtSvc))
		v1.GET("/statements", handlers.NewListStatementsHandler(stmtSvc))
	}

	// Legacy /api routes - also protected
	apiProtected := api.Group("")
	apiProtected.Use(authMiddleware)
	{
		apiProtected.GET("/plans",
			dep,
			auth.RequirePermission(auth.PermReadPlans),
			handlers.ListPlans,
		)

		apiProtected.GET("/subscriptions",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			handlers.ListSubscriptions,
		)

		apiProtected.GET("/subscriptions/:id",
			dep,
			auth.RequirePermission(auth.PermReadSubscriptions),
			handlers.GetSubscription,
		)

		apiProtected.GET("/statements/:id", handlers.NewGetStatementHandler(stmtSvc))
		apiProtected.GET("/statements", handlers.NewListStatementsHandler(stmtSvc))
	}

	admin := api.Group("/admin")
	admin.Use(authMiddleware)
	{
		admin.POST("/purge", adminHandler.PurgeCache)
		// Diagnostics endpoint — re-runs startup checks for live triage
		diagHandler := startup.NewDiagnosticsHandler(cfg, nil, nil)
		admin.GET("/diagnostics", auth.RequirePermission(auth.PermManageSubscriptions), diagHandler.Handle)
		
		// Reconciliation — scoped by RBAC and tenant
		adapter := reconciliation.NewMemoryAdapter()
		reconStore := reconciliation.NewMemoryStore()
		admin.POST("/reconcile", auth.RequirePermission(auth.PermManageSubscriptions), handlers.NewReconcileHandler(adapter, reconStore))
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
