package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/migrations"
	"stellarbill-backend/internal/routes"
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

	// Check if migrations should run on startup
	runMigrationsOnStartup := os.Getenv("RUN_MIGRATIONS") == "true"
	if runMigrationsOnStartup {
		if err := applyMigrationsOnStartup(&cfg); err != nil {
			log.Fatalf("migrations failed: %v", err)
		}
	}

	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	routes.Register(router)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeout) * time.Second,
	}

	log.Printf("server listening on %s", addr)
	if err := listenAndServe(srv); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

// applyMigrationsOnStartup loads and applies all pending migrations from the migrations directory.
// It fails fast with a clear error message if any migration fails.
func applyMigrationsOnStartup(cfg *config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Open database connection
	db, err := sql.Open("postgres", cfg.DBConn)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer db.Close()

	// Verify connectivity
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("database connection failed: %w", err)
	}

	// Load migrations from disk
	migs, err := migrations.LoadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to load migrations from migrations/: %w", err)
	}

	if len(migs) == 0 {
		log.Println("no migrations found to apply")
		return nil
	}

	// Create runner and apply migrations
	runner := migrations.Runner{DB: db}
	if err := runner.Validate(); err != nil {
		return fmt.Errorf("migration runner validation failed: %w", err)
	}

	applied, err := runner.Up(ctx, migs)
	if err != nil {
		return fmt.Errorf("migration execution failed: %w", err)
	}

	if len(applied) > 0 {
		log.Printf("successfully applied %d migration(s)", len(applied))
		for _, m := range applied {
			log.Printf("  - %d_%s", m.Version, m.Name)
		}
	} else {
		log.Println("no new migrations to apply (all already applied)")
	}

	return nil
}

func printConfigError(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
}
