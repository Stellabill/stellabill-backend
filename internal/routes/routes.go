package routes

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/featureflags"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/metrics"
	"stellarbill-backend/internal/middleware"
	"stellarbill-backend/internal/reconciliation"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/startup"
	"stellarbill-backend/internal/tracing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// Register configures all routes on the provided router.
func Register(r *gin.Engine) {
	_ = RegisterWithCleanup(r)
}

// RegisterWithCleanup configures all routes and returns a cleanup function for
// resources created during route wiring.
func RegisterWithCleanup(r *gin.Engine) func(context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		panic(fmt.Sprintf("failed to load configuration: %v", err))
	}

	var tracerShutdown func(context.Context) error

	// Initialize tracing
	if cfg.TracingExporter != "none" {
		tracerShutdown, err = tracing.InitTracer(cfg.TracingServiceName)
		if err != nil {
			fmt.Printf("Failed to initialize tracer: %v\n", err)
		}
	}

	// Global middleware
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery())
	r.Use(otelgin.Middleware(cfg.TracingServiceName))
	r.Use(middleware.TraceIDMiddleware())
	r.Use(metrics.MetricsMiddleware())

	r.Use(middleware.CORS(cfg.Env, cfg.AllowedOrigins))

	// Apply rate limiting middleware
	rateLimitConfig := middleware.RateLimiterConfig{
		Enabled:        cfg.RateLimitEnabled,
		Mode:           middleware.RateLimitMode(cfg.RateLimitMode),
		RequestsPerSec: int64(cfg.RateLimitRPS),
		BurstSize:      int64(cfg.RateLimitBurst),
		WhitelistPaths: append(cfg.RateLimitWhitelist, "/metrics"),
	}
	r.Use(middleware.RateLimitMiddleware(rateLimitConfig))

	var dbPool *pgxpool.Pool
	var planDB *sql.DB
	var replicaDB *sql.DB
	var routerDB db.DBTX

	if cfg.DBConn != "" {
		connectCtx, cancel := context.WithTimeout(
			context.Background(),
			time.Duration(cfg.DBPoolConnectTimeout)*time.Second,
		)
		dbPool, err = db.NewPool(connectCtx, cfg)
		cancel()

		planDB, err = sql.Open("postgres", cfg.DBConn)
		if err != nil {
			fmt.Printf("Failed to initialize plan database handle: %v\n", err)
		} else {
			repository.ApplySQLDBPoolConfig(planDB, cfg)
		}

		if cfg.DBReplicaConn != "" {
			replicaDB, err = sql.Open("postgres", cfg.DBReplicaConn)
			if err != nil {
				fmt.Printf("Failed to initialize replica database handle: %v\n", err)
			} else {
				repository.ApplySQLDBPoolConfig(replicaDB, cfg)
			}
		}

		if replicaDB != nil {
			routerDB = db.NewReadRouter(planDB, replicaDB)
		} else {
			routerDB = planDB
		}
	}

	var stopMetrics chan struct{}
	if dbPool != nil {
		stopMetrics = make(chan struct{})
		go func() {
			ticker := time.NewTicker(time.Duration(cfg.DBPoolMetricsInterval) * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					stats := dbPool.Stat()
					metrics.DBPoolMetrics.WithLabelValues("total_conns").Set(float64(stats.TotalConns()))
					metrics.DBPoolMetrics.WithLabelValues("idle_conns").Set(float64(stats.IdleConns()))
					metrics.DBPoolMetrics.WithLabelValues("active_conns").Set(float64(stats.TotalConns() - stats.IdleConns()))
					metrics.DBPoolMetrics.WithLabelValues("max_conns").Set(float64(stats.MaxConns()))
				case <-stopMetrics:
					return
				}
			}
		}()
	}

	var idemStore middleware.IdempotencyStore
	if dbPool != nil {
		idemStore = middleware.NewPostgresIdempotencyStore(dbPool)
	} else {
		idemStore = middleware.NewInMemoryIdempotencyStore()
	}
	idemMiddleware := middleware.Idempotency(idemStore)

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "dev-secret"
	}
	authMiddleware := middleware.AuthMiddleware(nil, jwtSecret)

	// Each cached repo gets its own InMemory cache instance so that Flush is
	// scoped to its namespace and does not evict entries from other caches.
	planCache := cache.NewInMemory()
	subCache := cache.NewInMemory()
	const repoCacheTTL = 5 * time.Minute

	var rawPlanRepo repository.PlanRepository = repository.NewMockPlanRepo()
	if routerDB != nil {
		pingCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		var pingErr error
		if planDB != nil {
			pingErr = planDB.PingContext(pingCtx)
		}
		cancel()

		if pingErr == nil {
			rawPlanRepo = repository.NewPostgresPlanRepo(routerDB)
		} else {
			fmt.Printf("Database connection failed (ping error: %v). Falling back to mock plan repository.\n", pingErr)
		}
	}
	rawSubRepo := repository.NewMockSubscriptionRepo(
		&repository.SubscriptionRow{ID: "sub-123", TenantID: "", CustomerID: "c1", Status: "active", PlanID: "p1", Amount: "1999", Currency: "USD", Interval: "monthly"},
		&repository.SubscriptionRow{ID: "sub-456", TenantID: "", CustomerID: "c2", Status: "active", PlanID: "p1", Amount: "1999", Currency: "USD", Interval: "monthly"},
		&repository.SubscriptionRow{ID: "test123", TenantID: "", CustomerID: "c3", Status: "active", PlanID: "p1", Amount: "1999", Currency: "USD", Interval: "monthly"},
	)

	cachedPlanRepo := repository.NewCachedPlanRepo(rawPlanRepo, planCache, repoCacheTTL)
	cachedSubRepo := repository.NewCachedSubscriptionRepo(rawSubRepo, subCache, repoCacheTTL)

	svc := service.NewSubscriptionService(cachedSubRepo, cachedPlanRepo)

	// Statement service wiring (in-memory mock for test/dev)
	stmtRepo := repository.NewMockStatementRepo()
	stmtSvc := service.NewStatementService(rawSubRepo, stmtRepo)

	// Fees and swap service wiring
	feeSvc := service.NewFeeService()
	feesHandler := handlers.NewFeesHandler(feeSvc)
	swapRouter := service.NewSwapRouter()
	swapHandler := handlers.NewSwapHandler(swapRouter)

	// handlerSubSvc adapts the mock repo to satisfy handlers.SubscriptionService.
	handlerSubSvc := &mockHandlerSubSvc{repo: rawSubRepo}
	// handlerPlanSvc adapts the cached plan repo to satisfy handlers.PlanService.
	handlerPlanSvc := &mockHandlerPlanSvc{repo: cachedPlanRepo}

	// Create handlers. The pool is wrapped in a PoolPinger so it satisfies
	// handlers.DBPinger (pgxpool exposes Ping, the health check wants
	// PingContext); readiness probes stay "not_configured" when no pool exists.
	var dbHealth handlers.DBPinger
	if dbPool != nil {
		dbHealth = &db.PoolPinger{Pool: dbPool}
	}
	h := handlers.NewHandlerWithDependencies(handlerPlanSvc, handlerSubSvc, dbHealth, nil)

	// Admin handler receives the cached repos so PurgeCache can invalidate them.
	adminToken := os.Getenv("ADMIN_TOKEN")
	adminHandler := handlers.NewAdminHandler(adminToken, cachedPlanRepo, cachedSubRepo)

	// Feature flags handler
	featureFlagsHandler := handlers.NewFeatureFlagsHandler(featureflags.GetInstance())

	// Wire the cached plan repo into the package-level ListPlans handler.
	handlers.SetPlanRepository(cachedPlanRepo)

	// API Groups
	api := r.Group("/api")
	api.GET("/metrics", gin.WrapH(promhttp.Handler()))
	v1 := api.Group("/v1")

	dep := middleware.DeprecationHeaders()

	// Public health check
	api.GET("/health", dep, h.LivenessProbe)
	v1.GET("/health", h.LivenessProbe)
	api.GET("/liveness", h.LivenessProbe)
	api.GET("/readiness", h.ReadinessProbe)

	// V1 routes are all protected
	v1.Use(authMiddleware)
	{
		v1.GET("/subscriptions", auth.RequirePermission(auth.PermReadSubscriptions), h.ListSubscriptions)
		v1.GET("/subscriptions/:id", auth.RequirePermission(auth.PermReadSubscriptions), h.GetSubscription)
		v1.POST("/subscriptions/:id/status", auth.RequirePermission(auth.PermManageSubscriptions), handlers.NewChangeSubscriptionStatusHandler(svc))
		v1.GET("/plans", auth.RequirePermission(auth.PermReadPlans), h.ListPlans)
		v1.GET("/statements/:id", handlers.NewGetStatementHandler(stmtSvc))
		v1.GET("/statements", handlers.NewListStatementsHandler(stmtSvc))

		// Fees module (#162)
		v1.GET("/fees/history", feesHandler.GetFeeHistory)

		// Swap router (#88)
		v1.POST("/swap/exact-in", swapHandler.SwapExactTokensForTokens)
		v1.POST("/swap/exact-out", swapHandler.SwapTokensForExactTokens)

		// Webhook attempt timeline (#362)
		attemptRepo := outbox.NewMemAttemptRepository()
		v1.GET("/webhooks/:id/attempts", auth.RequirePermission(auth.PermReadSubscriptions), handlers.NewWebhookAttemptsHandler(attemptRepo))
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
	{
		admin.POST("/purge", auth.RequirePermission(auth.PermManageSubscriptions), idemMiddleware, adminHandler.PurgeCache)
		// Diagnostics endpoint — re-runs startup checks for live triage
		diagHandler := startup.NewDiagnosticsHandler(cfg, nil, nil)
		admin.GET("/diagnostics", auth.RequirePermission(auth.PermManageSubscriptions), diagHandler.Handle)

		// Reconciliation — scoped by RBAC and tenant
		adapter := reconciliation.NewMemoryAdapter()
		reconStore := reconciliation.NewMemoryStore()
		admin.POST("/reconcile", auth.RequirePermission(auth.PermManageSubscriptions), idemMiddleware, handlers.NewReconcileHandler(adapter, reconStore))
		admin.GET("/reports", auth.RequirePermission(auth.PermReadReconciliation), handlers.NewListReportsHandler(reconStore))

		// Statement S3 export — admin or owning merchant only
		admin.POST("/statements/export", auth.RequirePermission(auth.PermManageSubscriptions), handlers.NewExportStatementsHandler(stmtSvc, nil))
	}


	return func(ctx context.Context) error {
		if dbPool != nil {
			log.Printf("closing database pool")
			dbPool.Close()
		}

		if tracerShutdown != nil {
			log.Printf("flushing tracer")
			if err := tracerShutdown(ctx); err != nil {
				return fmt.Errorf("shutdown tracer: %w", err)
			}
		}

		return nil
	}
}

		// Feature flags endpoints
		admin.GET("/feature-flags", auth.RequirePermission(auth.PermManageSubscriptions), featureFlagsHandler.GetFeatureFlags)
		admin.PATCH("/feature-flags", auth.RequirePermission(auth.PermManageSubscriptions), idemMiddleware, featureFlagsHandler.ToggleFeatureFlag)
	}

	return func(ctx context.Context) error {
		if stopMetrics != nil {
			close(stopMetrics)
		}
		if dbPool != nil {
			log.Printf("closing database pool")
			dbPool.Close()
		}
		if planDB != nil {
			log.Printf("closing plan database handle")
			if err := planDB.Close(); err != nil {
				return fmt.Errorf("close plan database handle: %w", err)
			}
		}
		if replicaDB != nil {
			log.Printf("closing replica database handle")
			if err := replicaDB.Close(); err != nil {
				return fmt.Errorf("close replica database handle: %w", err)
			}
		}
		if tracerShutdown != nil {
			log.Printf("flushing tracer")
			if err := tracerShutdown(ctx); err != nil {
				return fmt.Errorf("shutdown tracer: %w", err)
			}
		}
		return nil
	}
}

func applyPGXPoolConfig(poolConfig *pgxpool.Config, cfg config.Config) {
	if poolConfig == nil {
		return
	}

	poolConfig.MaxConns = int32(cfg.DBPoolMaxConns)
	poolConfig.MinConns = int32(cfg.DBPoolMinConns)
	poolConfig.MaxConnLifetime = time.Duration(cfg.DBPoolMaxConnLifetime) * time.Second
	poolConfig.MaxConnIdleTime = time.Duration(cfg.DBPoolMaxConnIdleTime) * time.Second
	poolConfig.HealthCheckPeriod = time.Duration(cfg.DBPoolHealthCheckPeriod) * time.Second
	poolConfig.ConnConfig.ConnectTimeout = time.Duration(cfg.DBPoolConnectTimeout) * time.Second
}

// mockHandlerSubSvc adapts *repository.MockSubscriptionRepo to handlers.SubscriptionService.
type mockHandlerSubSvc struct {
	repo *repository.MockSubscriptionRepo
}

func (m *mockHandlerSubSvc) ListSubscriptions(_ *gin.Context) ([]handlers.Subscription, error) {
	rows := m.repo.All()
	out := make([]handlers.Subscription, 0, len(rows))
	for _, r := range rows {
		out = append(out, handlers.Subscription{
			ID:          r.ID,
			PlanID:      r.PlanID,
			Customer:    r.CustomerID,
			Status:      r.Status,
			Amount:      r.Amount,
			Interval:    r.Interval,
			NextBilling: r.NextBilling,
		})
	}
	return out, nil
}

func (m *mockHandlerSubSvc) GetSubscription(_ *gin.Context, id string) (*handlers.Subscription, error) {
	r, err := m.repo.FindByID(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return &handlers.Subscription{
		ID:          r.ID,
		PlanID:      r.PlanID,
		Customer:    r.CustomerID,
		Status:      r.Status,
		Amount:      r.Amount,
		Interval:    r.Interval,
		NextBilling: r.NextBilling,
	}, nil
}

// mockHandlerPlanSvc adapts a PlanRepository to handlers.PlanService.
type mockHandlerPlanSvc struct {
	repo repository.PlanRepository
}

func (m *mockHandlerPlanSvc) ListPlans(_ *gin.Context) ([]handlers.Plan, error) {
	rows, err := m.repo.List(context.Background())
	if err != nil {
		return nil, err
	}
	out := make([]handlers.Plan, 0, len(rows))
	for _, r := range rows {
		out = append(out, handlers.Plan{
			ID:          r.ID,
			Name:        r.Name,
			Amount:      r.Amount,
			Currency:    r.Currency,
			Interval:    r.Interval,
			Description: r.Description,
		})
	}
	return out, nil
}