package routes

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/metrics"
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
	"github.com/jackc/pgx/v5/stdlib"
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
	r.Use(middleware.FaultInjection())
	r.Use(otelgin.Middleware(cfg.TracingServiceName))
	r.Use(middleware.TailSamplingSignals())
	r.Use(middleware.TraceIDMiddleware())

	// Prometheus HTTP metrics — observes per-request latency and counts by
	// route, method, and status for every request entering the router, using
	// c.FullPath() so label cardinality stays bounded by the route table.
	// Exposed via the dedicated /metrics handler below (no auth; IP-restricted
	// by network policy).
	r.Use(metrics.MetricsMiddleware())

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
	auditLogger := audit.NewLogger(cfg.JWTSecret, audit.NewSinkFromEnv())
	if auditLogger != nil {
		r.Use(audit.Middleware(auditLogger))
	}

	var dbPinger handlers.DBPinger
	if pool, err := db.NewPool(context.Background(), cfg); err == nil && pool != nil {
		breakerPool := db.NewBreakerPoolFromConfig(pool, cfg)
		dbPinger = breakerPool
	} else if err != nil {
		log.Printf("db health: database pool unavailable: %v", err)
	}

	r.Use(middleware.DataLoaderMiddleware(planRepo, subRepo))

	stmtSvc := service.NewStatementService(subRepo, stmtRepo)
	svc := service.NewSubscriptionService(subRepo, planRepo)

	// Create handlers
	h := handlers.NewHandler(nil, nil)
	if breakerPool, ok := dbPinger.(*db.BreakerPool); ok {
		h.Database = breakerPool
	}
	adminHandler := handlers.NewAdminHandler(cfg.AdminToken)
	exportSvc := service.NewTenantExportService(planRepo, subRepo, stmtRepo)
	exportJobManager := service.NewExportJobManager(exportSvc, noopS3Uploader{}, nil)

	// Auth configuration
	jwtSecret := cfg.JWTSecret
	authMiddleware := middleware.AuthMiddleware(nil, jwtSecret)

	// Per-tenant rate limiting — layered after the global rate limiter on all
	// authenticated routes. Enabled only when RATE_LIMIT_TENANT_RPS is set.
	tenantRateLimiter := middleware.TenantRateLimitMiddleware(middleware.TenantRateLimitConfig{
		Enabled: cfg.RateLimitTenantRPS > 0,
		RPS:     cfg.RateLimitTenantRPS,
		Burst:   cfg.RateLimitTenantBurst,
	})

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
	v1.Use(tenantRateLimiter)
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
	apiProtected.Use(tenantRateLimiter)
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

	// Webhook routes — provider-specific endpoints with HMAC verification.
	// Signing secrets are resolved per-provider from the secrets chain
	// (e.g. Vault), falling back to WEBHOOK_SECRET_<PROVIDER> env vars.
	secretProvider := secrets.NewDefaultProvider()

	// Outbox store persists verified webhook events for asynchronous
	// processing. It stays nil when no database is configured (dev mode);
	// the handler responds 503 in that case rather than panicking.
	var outboxStore outbox.Repository
	if cfg.DBConn != "" {
		if dbPool, err := db.NewPool(context.Background(), cfg); err == nil && dbPool != nil {
			outboxStore = outbox.NewPostgresRepository(stdlib.OpenDBFromPool(dbPool))
		}
	}

	webhookHandler := handlers.NewWebhookHandler(outboxStore)

	webhookGroup := r.Group("/api/webhooks")
	{
		stripeMiddleware, err := webhookVerificationFor(secretProvider, middleware.ProviderStripe, "WEBHOOK_SECRET_STRIPE")
		if err != nil {
			fmt.Printf("WARNING: failed to register stripe webhook route: %v\n", err)
		} else {
			webhookGroup.POST("/stripe", stripeMiddleware, webhookHandler.Receive)
		}

		genericMiddleware, err := webhookVerificationFor(secretProvider, middleware.ProviderGeneric, "WEBHOOK_SECRET_GENERIC")
		if err != nil {
			fmt.Printf("WARNING: failed to register generic webhook route: %v\n", err)
		} else {
			webhookGroup.POST("/generic", genericMiddleware, webhookHandler.Receive)
		}
	}

	// Legacy webhook endpoint (backward compatibility) — plain HMAC-SHA256
	// signed with the legacy shared WEBHOOK_SECRET.
	legacySecret := os.Getenv("WEBHOOK_SECRET")
	r.POST("/webhooks", middleware.WebhookVerification(legacySecret), webhookHandler.Receive)

	// Admin login (no JWT required — uses admin token directly)
	r.POST("/api/admin/login", adminHandler.Login)

	// Redacted config dump — accessible via admin JWT or admin token header
	r.GET("/internal/config-dump", handlers.ConfigDumpHandler(&cfg))

	admin := api.Group("/admin")
	admin.Use(authMiddleware)

	{
		admin.POST("/purge", adminHandler.PurgeCache)
		// Diagnostics endpoint — re-runs startup checks for live triage
		diagHandler := startup.NewDiagnosticsHandler(cfg, dbPinger, nil)
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

		// Outbox dead-letter inspection and manual recovery. Both endpoints are
		// gated behind manage:reconciliation (admin-only role) and the requeue
		// endpoint is idempotency-keyed so a retried POST cannot reset an
		// already-requeued event a second time.
		outboxAdmin := handlers.NewOutboxAdminHandler(newOutboxRepository(cfg))
		admin.GET("/outbox/dead-letter",
			auth.RequirePermission(auth.PermManageReconciliation),
			outboxAdmin.ListDeadLetteredEvents)
		admin.POST("/outbox/:id/requeue",
			auth.RequirePermission(auth.PermManageReconciliation),
			middleware.Idempotency(nil),
			outboxAdmin.RequeueOutboxEvent)
	}
}

// webhookVerificationFor builds signature-verification middleware for a
// provider, resolving the per-provider secret exclusively from the secrets
// chain (Vault first, env provider as a fallback inside the abstraction).
// Secrets are never read from a raw environment variable in this package.
// When no secret is configured the endpoint is registered with middleware
// that rejects every request with 403 so it can never be accepted using a
// placeholder secret.
func webhookVerificationFor(provider secrets.Provider, webhookProvider middleware.WebhookProvider, secretKey string) (gin.HandlerFunc, error) {
	secret, err := provider.GetSecret(context.Background(), secretKey)
	if err != nil || secret == "" {
		return func(c *gin.Context) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"error":   "webhook_secret_not_configured",
				"message": "webhook secret is not configured for this endpoint",
			})
		}, nil
	}

	cfg := middleware.ProviderConfig(webhookProvider)
	cfg.SecretKey = secret
	return middleware.WebhookVerificationMiddleware(cfg)
}

type noopS3Uploader struct{}

// newOutboxRepository builds an outbox.Repository backed by a dedicated
// pgx pool. It mirrors the defensive pattern used by startAnalyzeJob: when the
// database is not configured (empty DBConn) or unreachable it returns a nil
// repository, and the admin outbox handlers respond 503 rather than failing
// server boot.
func newOutboxRepository(cfg config.Config) outbox.Repository {
	pool, err := db.NewPool(context.Background(), cfg)
	if err != nil {
		log.Printf("outbox admin: db pool creation failed: %v", err)
		return nil
	}
	if pool == nil {
		return nil
	}
	return outbox.NewPostgresPgxRepository(pool)
}

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
