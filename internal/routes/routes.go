package routes

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/startup"
	"stellarbill-backend/internal/storage/s3"
	"stellarbill-backend/internal/tracing"
	"stellarbill-backend/internal/worker"

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

	// Start the ANALYZE background job to keep table statistics fresh.
	// Uses pgxpool so ANALYZE runs on the same connection pool as the rest
	// of the application, avoiding a separate connection.
	startAnalyzeJob(cfg)

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

	// Per-endpoint concurrency shedding — shed excess load before rate limiting
	if cfg.ConcurrencyCapsPath != "" {
		concCfg, err := middleware.LoadConcurrencyConfig(cfg.ConcurrencyCapsPath)
		if err != nil {
			fmt.Printf("WARNING: failed to load concurrency caps config from %s: %v\n", cfg.ConcurrencyCapsPath, err)
		} else {
			r.Use(middleware.InflightMiddleware(concCfg))
		}
	}

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

	r.Use(middleware.DataLoaderMiddleware(planRepo, subRepo))

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
		v1.PATCH("/subscriptions/:id", h.PatchSubscription)
		v1.GET("/subscriptions/:id/events", h.GetSubscriptionEvents)
		v1.GET("/plans", h.ListPlans)
		v1.PATCH("/plans/:id", h.PatchPlan)
		v1.GET("/statements/:id", handlers.NewGetStatementHandler(stmtSvc))
		v1.GET("/statements", handlers.NewListStatementsHandler(stmtSvc))
		v1.POST("/tenants/me/export", handlers.NewTenantExportHandler(exportJobManager))
		v1.GET("/operations/:id", handlers.NewOperationStatusHandler(exportJobManager))
	}

	// CSP violation reports — public (no auth; browsers send without tokens),
	// but per-tenant rate limited to prevent DoS amplification.
	cspRateLimiter := middleware.TenantRateLimitMiddleware(middleware.TenantRateLimitConfig{
		Enabled: true,
		RPS:     cfg.CSPReportRPS,
		Burst:   cfg.CSPReportBurst,
	})
	v1.POST("/csp-reports", cspRateLimiter, middleware.CSPReportHandler())

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

	// Redacted config dump — accessible via admin JWT or admin token header
	r.GET("/internal/config-dump", handlers.ConfigDumpHandler(&cfg))

	admin := api.Group("/admin")
	admin.Use(authMiddleware)

	{
		admin.POST("/purge", adminHandler.PurgeCache)
		// Diagnostics endpoint — re-runs startup checks for live triage
		diagHandler := startup.NewDiagnosticsHandler(cfg, nil, nil)
		admin.GET("/diagnostics", auth.RequirePermission(auth.PermManageSubscriptions), diagHandler.Handle)

		// Redacted config dump under admin group with RBAC
		admin.GET("/config-dump", auth.RequirePermission(auth.PermManageSubscriptions), handlers.ConfigDumpHandler(&cfg))

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

// startAnalyzeJob initializes the database pool and starts the periodic ANALYZE
// background job. If the pool cannot be created (e.g. no DATABASE_URL in
// development), it logs a warning and skips the job rather than failing
// the entire startup.
//
// Note: The pool and job are intentionally not torn down on graceful shutdown.
// The pool is long-lived (matching the server process lifetime) and ANALYZE is
// non-blocking, so immediate termination on process exit is safe. A shutdown
// hook can be added later if needed.
func startAnalyzeJob(cfg config.Config) {
	pool, err := db.NewPool(context.Background(), cfg)
	if err != nil {
		log.Printf("analyze job: skipping — db pool creation failed: %v", err)
		return
	}
	if pool == nil {
		log.Println("analyze job: skipping — no database configured (empty DATABASE_URL)")
		return
	}

	analyzeJob := worker.NewAnalyzeJob(pool, worker.DefaultAnalyzeConfig(), analyzeLogger{})
	analyzeJob.Start()
	log.Println("analyze job: started periodic ANALYZE for hot tables (outbox_events, statements, subscriptions)")
}

// analyzeLogger adapts the standard log package to the worker's analyzeLogger
// interface so the ANALYZE job can emit structured error messages.
type analyzeLogger struct{}

func (analyzeLogger) Error(msg string, keysAndValues ...any) {
	log.Printf("ERROR: %s %v", msg, keysAndValues)
}

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
