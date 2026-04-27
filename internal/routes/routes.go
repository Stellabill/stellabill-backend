package routes

import (
	"fmt"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/tracing"
	"stellarbill-backend/internal/reconciliation"

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
	
	// Create the main handler instance
	h := handlers.NewHandler(nil, nil) // Placeholder for services if needed
	h.Database = nil // Wire real DB here
	
	// Auth configuration
	jwtSecret := cfg.JWTSecret
	authMW := middleware.AuthMiddleware(nil, jwtSecret)

	// API Groups
	api := r.Group("/api")
	v1 := api.Group("/v1")
	
	dep := middleware.DeprecationHeaders()

	// Public Routes
	api.GET("/health", h.ReadinessProbe)
	api.GET("/liveness", h.LivenessProbe)

	// Protected Routes (v1)
	v1.Use(authMW)
	{
		v1.GET("/plans", dep, h.ListPlans)
		v1.GET("/subscriptions", dep, h.ListSubscriptions)
		v1.GET("/subscriptions/:id", dep, h.GetSubscription)
		
		// Hardened Statements API
		v1.GET("/statements", dep, handlers.NewListStatementsHandler(stmtSvc))
		v1.GET("/statements/:id", dep, handlers.NewGetStatementHandler(stmtSvc))
	}

	// Admin Routes
	admin := api.Group("/admin")
	admin.Use(authMW) // Should ideally be a more restrictive check
	{
		adminHandler := handlers.NewAdminHandler(cfg.AdminToken)
		admin.POST("/purge", adminHandler.PurgeCache)
		
		// Reconciliation — scoped by RBAC and tenant
		adapter := reconciliation.NewMemoryAdapter()
		reconStore := reconciliation.NewMemoryStore()
		admin.POST("/reconcile", auth.RequirePermission(auth.PermManageReconciliation), handlers.NewReconcileHandler(adapter, reconStore))
		admin.GET("/reports", auth.RequirePermission(auth.PermReadReconciliation), handlers.NewListReportsHandler(reconStore))
	}
}
