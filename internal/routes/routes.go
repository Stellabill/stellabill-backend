package routes

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/outbox"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/secrets"
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
	register(r, cfg)
}

// RegisterWithCleanup configures all routes on the provided router and returns
// a cleanup function that stops background jobs started during registration.
func RegisterWithCleanup(r *gin.Engine) (func(context.Context) error, error) {
	cfg, err := config.Load()
	if err != nil {
		return func(context.Context) error { return nil }, err
	}
	return register(r, cfg), nil
}

// register wires the complete route table. The returned cleanup function
// tears down background jobs created from the configuration.
func register(r *gin.Engine, cfg config.Config) func(context.Context) error {
	var stops []func() error

	// Start the ANALYZE background job to keep table statistics fresh.
	// Uses pgxpool so ANALYZE runs on the same connection pool as the rest
	// of the application, avoiding a separate connection.
	stops = append(stops, startAnalyzeJob(cfg))

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
	r.Use(middleware.FaultInjection())
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

	r.Use(repository.DataLoaderMiddleware(planRepo, subRepo))

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

	// Webhook receivers — inbound provider events are HMAC-verified by
	// provider-specific middleware and persisted to the outbox for async
	// processing. Replays are rejected via the event-ID cache in the
	// verification middleware (middleware.WebhookVerificationMiddleware).
	// The legacy /webhooks route is retained for backward compatibility.
	webhookHandler := handlers.NewWebhookHandler(webhookOutboxStore(cfg))
	r.POST("/api/webhooks/stripe",
		webhookReceiverMiddleware(middleware.ProviderStripe, cfg),
		webhookHandler.Receive,
	)
	r.POST("/api/webhooks/generic",
		webhookReceiverMiddleware(middleware.ProviderGeneric, cfg),
		webhookHandler.Receive,
	)
	webhookSecret := os.Getenv("WEBHOOK_SECRET")
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

	return func(ctx context.Context) error {
		var errs []error
		for _, stop := range stops {
			if err := stop(); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
}

type noopS3Uploader struct{}

// startAnalyzeJob initializes the database pool and starts the periodic ANALYZE
// background job. If the pool cannot be created (e.g. no DATABASE_URL in
// development), it logs a warning and skips the job rather than failing
// the entire startup.
//
// The returned stop function tears down the analyze job and closes the pool.
// If no job could be started it is a no-op.
func startAnalyzeJob(cfg config.Config) func() error {
	pool, err := db.NewPool(context.Background(), cfg)
	if err != nil {
		log.Printf("analyze job: skipping — db pool creation failed: %v", err)
		return func() error { return nil }
	}
	if pool == nil {
		log.Println("analyze job: skipping — no database configured (empty DATABASE_URL)")
		return func() error { return nil }
	}

	analyzeJob := worker.NewAnalyzeJob(pool, worker.DefaultAnalyzeConfig(), analyzeLogger{})
	analyzeJob.Start()
	log.Println("analyze job: started periodic ANALYZE for hot tables (outbox_events, statements, subscriptions)")

	return func() error {
		if err := analyzeJob.Stop(); err != nil {
			return err
		}
		pool.Close()
		return nil
	}
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

// webhookOutboxStore returns an outbox repository for webhook persistence, or
// nil when no database is configured (dev mode), in which case the receiver
// validates and acknowledges events without persisting them.
func webhookOutboxStore(cfg config.Config) outbox.Repository {
	pool, err := db.NewPool(context.Background(), cfg)
	if err != nil || pool == nil {
		if err != nil {
			log.Printf("webhook receiver: no outbox store (db pool: %v); events will be acknowledged without persistence", err)
		}
		return nil
	}
	return outbox.NewPostgresPgxRepository(pool)
}

// webhookReceiverMiddleware builds the provider-specific webhook verification
// middleware. The signing secret is resolved through the secrets provider
// (WEBHOOK_SECRET_<PROVIDER>), falling back to the legacy WEBHOOK_SECRET
// environment variable for backward compatibility.
func webhookReceiverMiddleware(provider middleware.WebhookProvider, cfg config.Config) gin.HandlerFunc {
	secret := os.Getenv("WEBHOOK_SECRET")
	if p := secrets.NewDefaultProvider(); p != nil {
		key := "WEBHOOK_SECRET_" + strings.ToUpper(provider.String())
		if resolved, err := p.GetSecret(context.Background(), key); err == nil && resolved != "" {
			secret = resolved
		}
	}

	whCfg := middleware.ProviderConfig(provider)
	whCfg.SecretKey = secret
	if whCfg.SecretKey == "" {
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "webhook_receiver_unavailable",
				"message": "webhook secret is not configured for provider " + provider.String(),
			})
		}
	}

	mw, err := middleware.WebhookVerificationMiddleware(whCfg)
	if err != nil {
		log.Printf("webhook receiver: provider %s disabled: %v", provider, err)
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "webhook_receiver_unavailable",
				"message": err.Error(),
			})
		}
	}
	return mw
}
