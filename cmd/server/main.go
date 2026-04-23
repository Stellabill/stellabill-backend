package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"database/sql"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/routes"
	"stellarbill-backend/internal/shutdown"
	"stellarbill-backend/internal/startup"
)

var listenAndServe = func(srv *http.Server) error {
	return srv.ListenAndServe()
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		printConfigError(err)
		os.Exit(1)
	}

	// Init PII-safe logger
	var logger *zap.Logger
	if cfg.Env == "production" {
		logger = security.ProductionLogger()
		defer logger.Sync()
		gin.SetMode(gin.ReleaseMode)
		logger.Info("Running in production mode")
	} else if cfg.Env == "development" {
		logger = security.DevLogger()
		defer logger.Sync()
		gin.SetMode(gin.DebugMode)
		logger.Info("Running in development mode")
	} else {
		logger = security.ProductionLogger()
		defer logger.Sync()
		gin.SetMode(gin.TestMode)
		logger.Info("Running in test mode", zap.String("env", cfg.Env))
	}

	// Log config warnings
	if vResult := cfg.Validate(); len(vResult.Warnings) > 0 {
		logger.Warn("Configuration warnings",
			zap.Strings("warnings", vResult.Warnings))
	}

	// Run startup diagnostics — fail fast if critical checks fail
	startupResults := startup.RunChecks(cfg, nil, nil) // db/migrations wired below once available
	logger.Info("Startup checks completed:\n" + startup.FormatResults(startupResults))
	if startup.HasFailures(startupResults) {
		logger.Fatal("Startup checks failed — aborting")
	}

	// Create router with configured middleware
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.Logger(logger))

	// Security headers middleware
	router.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Next()
	})

	// Wire up services and handlers, then register routes
	planSvc := services.NewPlanService()
	subSvc := services.NewSubscriptionService()
	h := handlers.NewHandler(planSvc, subSvc)
	routes.Register(router, h)

	// Build server address
	addr := fmt.Sprintf(":%d", cfg.Port)

	// Create HTTP server with configuration
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeout) * time.Second,
	}

	logger.Info("Starting Stellarbill backend",
		zap.String("addr", addr),
		zap.String("env", cfg.Env))
	logger.Info("Server timeouts",
		zap.Int("read", cfg.ReadTimeout),
		zap.Int("write", cfg.WriteTimeout),
		zap.Int("idle", cfg.IdleTimeout))

	// Initialize graceful shutdown orchestrator
	// Shutdown timeout: 30 seconds (total time for all cleanup)
	// Drain timeout: 20 seconds (time to wait for in-flight requests)
	gracefulShutdown := shutdown.NewGracefulShutdown(
		srv,
		30*time.Second,
		20*time.Second,
	)

	// Register cleanup callbacks in reverse order of initialization
	gracefulShutdown.OnShutdown(func(ctx context.Context) error {
		log.Println("Cleanup callback: Releasing resources...")
		// Add any additional cleanup logic here
		// e.g., database connection pools, caches, etc.
		return nil
	})

	// Start server in a background goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Println("Listening for connections...")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	// Start signal listener (blocks until shutdown is triggered)
	go gracefulShutdown.ListenForShutdownSignals()

	// Wait for either a server error or shutdown completion
	select {
	case err := <-serverErrors:
		log.Fatalf("Server error: %v", err)
	case <-func() <-chan struct{} {
		// Wait for graceful shutdown to complete
		done := make(chan struct{})
		go func() {
			gracefulShutdown.Wait()
			close(done)
		}()
		return done
	}():
		log.Println("Server shutdown completed successfully")
	}

	logger.Init()

	r := gin.New()

	r.Use(middleware.RecoveryLogger())

	var db *sql.DB = nil // existing or future DB

	routes.RegisterRoutes(r, db)

	r.Run()
}

func newRouter() *gin.Engine {
	router := gin.New()
	router.Use(
		middleware.Recovery(log.Default()),
		middleware.RequestID(),
		middleware.Logging(log.Default()),
		middleware.CORS("*"),
		middleware.RateLimit(middleware.NewRateLimiter(60, time.Minute)),
	)
	routes.Register(router)
	return router
}

