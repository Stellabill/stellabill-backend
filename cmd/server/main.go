package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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

const shutdownTimeout = 30 * time.Second

type cleanupFunc func(context.Context) error

func main() {
	if err := run(); err != nil {
		log.Printf("server shutdown failed: %v", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		printConfigError(err)
		return err
	}

	// Check if migrations should run on startup
	runMigrationsOnStartup := os.Getenv("RUN_MIGRATIONS") == "true"
	if runMigrationsOnStartup {
		if err := applyMigrationsOnStartup(&cfg); err != nil {
			return fmt.Errorf("migrations failed: %w", err)
		}
	}

	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	cleanup := routes.RegisterWithCleanup(router)

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeout) * time.Second,
	}

	shutdownCtx, secondSignal, stopSignals := notifyShutdownSignals()
	defer stopSignals()

	return runHTTPServer(shutdownCtx, secondSignal, srv, shutdownTimeout, cleanup)
}

func notifyShutdownSignals() (context.Context, <-chan os.Signal, func()) {
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-sigCh
		cancel()
	}()

	stop := func() {
		signal.Stop(sigCh)
		cancel()
	}

	return ctx, sigCh, stop
}

func runHTTPServer(ctx context.Context, secondSignal <-chan os.Signal, srv *http.Server, timeout time.Duration, cleanup cleanupFunc) error {
	serverErr := make(chan error, 1)
	go func() {
		log.Printf("server listening on %s", srv.Addr)
		serverErr <- listenAndServe(srv)
	}()

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case <-ctx.Done():
		log.Printf("shutdown signal received; allowing up to %s for graceful shutdown", timeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	shutdownErr := make(chan error, 1)
	go func() {
		shutdownErr <- srv.Shutdown(shutdownCtx)
	}()

	var err error
	select {
	case sig := <-secondSignal:
		log.Printf("second shutdown signal received (%s); forcing server close", sig)
		err = errors.Join(fmt.Errorf("forced shutdown after second signal: %s", sig), srv.Close())
	case shutdownResult := <-shutdownErr:
		if shutdownResult != nil {
			log.Printf("http server graceful shutdown failed: %v", shutdownResult)
			err = errors.Join(err, fmt.Errorf("http server shutdown: %w", shutdownResult))
			err = errors.Join(err, srv.Close())
		} else {
			log.Printf("http server stopped accepting requests and drained in-flight work")
		}
	}

	if cleanup != nil {
		if cleanupErr := cleanup(shutdownCtx); cleanupErr != nil {
			log.Printf("server cleanup failed: %v", cleanupErr)
			err = errors.Join(err, fmt.Errorf("server cleanup: %w", cleanupErr))
		} else {
			log.Printf("server cleanup completed")
		}
	}

	select {
	case serveErr := <-serverErr:
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			err = errors.Join(err, fmt.Errorf("server error: %w", serveErr))
		}
	case <-time.After(time.Second):
		err = errors.Join(err, errors.New("server did not stop after shutdown"))
	}

	if err != nil {
		return err
	}

	log.Printf("graceful shutdown completed")
	return nil
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
