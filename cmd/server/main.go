package main

import (
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
	"stellarbill-backend/internal/services"
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
}

	// Log config warnings
	if vResult := cfg.Validate(); len(vResult.Warnings) > 0 {
		logger.Warn("Configuration warnings",
			zap.Strings("warnings", vResult.Warnings))
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

	// Start server with fail-fast behavior
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatal("Failed to start server", zap.Error(err))
	}

	logger.Init()

	r := gin.New()

	r.Use(middleware.RecoveryLogger())
	r.Use(middleware.RequestLogger())

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

