package routes

import (
	"context"
	"fmt"
	"os"
	"time"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/startup"
	"stellarbill-backend/internal/storage/s3"
	"stellarbill-backend/internal/tracing"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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
	r.Use(middleware.TailSamplingSignals())
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
	planRepo := repository.NewMockPlanRepo()
	stmtRepo := repository.NewMockStatementRepo()

	stmtSvc := service.NewStatementService(subRepo, stmtRepo)
	svc := service.NewSubscriptionService(subRepo, planRepo)

	// Create handlers
	h := handlers.NewHandler(nil, nil)
	adminHandler := handlers.NewAdminHandler(cfg.AdminToken)
	exportSvc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	exportJobManager := service.NewExportJobManager(exportSvc, noopS3Uploader{}, nil)

	// Auth configuration
	jwtSecret := cfg.JWTSecret
	authMiddleware := middleware.AuthMiddleware(nil, jwtSecret)

	// API Groups
	api := r.Group("/api")
	v1 := api.Group("/v1")

	dep := middleware.DeprecationHeaders()

	// Public health check
	api.GET("/health", dep, h.LivenessProbe)
	v1.GET("/health", h.LivenessProbe)
	api.GET("/liveness", h.LivenessProbe)
	api.GET("/readiness", h.ReadinessProbe)

	// Prometheus metrics — no auth required; network-level access control is
	// expected (e.g. Kubernetes NetworkPolicy or reverse-auth proxy).
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// V1 routes are all protected
	v1.Use(authMiddleware)
	{
		v1.GET("/subscriptions", h.ListSubscriptions)
		v1.GET("/subscriptions/:id", handlers.NewGetSubscriptionHandler(svc))
		v1.GET("/subscriptions/:id/events", h.GetSubscriptionEvents)
		v1.GET("/plans", h.ListPlans)
		v1.GET("/statements/:id", handlers.NewGetStatementHandler(stmtSvc))
		v1.GET("/statements", handlers.NewListStatementsHandler(stmtSvc))
		v1.POST("/tenants/me/export", handlers.NewTenantExportHandler(exportJobManager))
		v1.GET("/operations/:id", handlers.NewOperationStatusHandler(exportJobManager))
	}

	// Legacy /api routes - also protected
	apiProtected := api.Group("")
	apiProtected.Use(authMiddleware)
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

		apiProtected.GET("/statements/:id", handlers.NewGetStatementHandler(stmtSvc))
		apiProtected.GET("/statements", handlers.NewListStatementsHandler(stmtSvc))
	}

	// Webhook receiver — signature verified by WebhookVerification middleware
	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	webhookHandler := handlers.NewWebhookHandler()
	r.POST("/webhooks", middleware.WebhookVerification(webhookSecret), webhookHandler.Receive)
	// Admin login (no JWT required — uses admin token directly)
	r.POST("/api/admin/login", adminHandler.Login)

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

type noopS3Uploader struct{}

func (noopS3Uploader) PutObject(context.Context, string, []byte, string) error { return nil }
func (noopS3Uploader) PresignURL(context.Context, string, time.Duration) (s3.PresignedURL, error) {
	return s3.PresignedURL{URL: "", ExpiresAt: time.Time{}}, nil
}

func (m *mockHandlerPlanSvc) PatchPlan(c *gin.Context, id string, plan *handlers.Plan, expectedVersion int64) error {
	repoPlan := &repository.PlanRow{
		ID:          id,
		Name:        plan.Name,
		Description: plan.Description,
	}
	return m.repo.Update(c.Request.Context(), repoPlan, expectedVersion)
}

func (m *mockHandlerPlanSvc) DeletePlan(c *gin.Context, id string, expectedVersion int64) error {
	return m.repo.Delete(c.Request.Context(), id, expectedVersion)
}
