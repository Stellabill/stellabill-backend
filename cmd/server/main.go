package main

import (
	"fmt"
	"log"
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

func main() {
	// Load configuration with strict validation
	cfg, err := config.Load()
	if err != nil {
		// Fail fast with descriptive error
		fmt.Fprintf(os.Stderr, "ERROR: Configuration validation failed: %s\n", err.Error())
		fmt.Fprintln(os.Stderr, "\nRequired environment variables:")
		for _, key := range config.GetRequiredEnvVars() {
			fmt.Fprintf(os.Stderr, "  - %s\n", key)
		}
		fmt.Fprintln(os.Stderr, "\nOptional environment variables and defaults:")
		for key, val := range config.GetOptionalEnvVars() {
			fmt.Fprintf(os.Stderr, "  - %s (default: %s)\n", key, val)
		}
		os.Exit(1)
	}

	// Log warnings if any
	if vResult := cfg.Validate(); len(vResult.Warnings) > 0 {
		log.Printf("WARNING: Configuration warnings:")
		for _, w := range vResult.Warnings {
			log.Printf("  - %s", w)
		}
	}

	// Set Gin mode based on environment
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
		log.Println("Running in production mode")
	} else if cfg.Env == "development" {
		gin.SetMode(gin.DebugMode)
		log.Println("Running in development mode")
	} else {
		gin.SetMode(gin.TestMode)
		log.Printf("Running in %s mode", cfg.Env)
	}

	// Create router with configured middleware
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())

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

	log.Printf("Starting Stellarbill backend on %s (env: %s)", addr, cfg.Env)
	log.Printf("Server timeouts - Read: %ds, Write: %ds, Idle: %ds", 
		cfg.ReadTimeout, cfg.WriteTimeout, cfg.IdleTimeout)

	// Start server with fail-fast behavior
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Failed to start server: %v", err)
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
