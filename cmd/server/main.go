package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/routes"
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

	// Create router with configured timeouts
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())

	// Set timeouts from configuration
	router.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Next()
	})

	// Register routes
	routes.Register(router)

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
}
