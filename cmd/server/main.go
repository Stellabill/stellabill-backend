package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/metrics"
	"stellarbill-backend/internal/routes"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
)

var (
	listenAndServe = func(srv *http.Server) error {
		return srv.ListenAndServe()
	}

	// openDB is a variable so tests can inject a mock.
	openDB = func(driver, connStr string) (*sql.DB, error) {
		return sql.Open(driver, connStr)
	}
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		printConfigError(err)
		os.Exit(1)
	}

	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	spiffeVerifier, err := auth.NewSpiffeVerifier(context.Background(), cfg.SpiffeSocketPath, cfg.SpiffeTrustDomain, cfg.Env)
	if err != nil {
		log.Fatalf("failed to initialize SPIFFE verifier: %v", err)
	}
	if spiffeVerifier != nil {
		if v, ok := spiffeVerifier.(interface{ Close() }); ok {
			defer v.Close()
		}
	}

	// Start KPI metrics refresh worker when a database is available.
	var kpiJob *worker.KpiRefreshJob
	if db != nil {
		kpiJob = worker.NewKpiRefreshJob(db, worker.DefaultKpiRefreshConfig(), stdLogger{})
		kpiJob.Start()
		log.Println("KPI metrics refresh worker started (hourly)")
	}

	router := gin.New()
	router.Use(gin.Recovery())

	routes.Register(router)

	// Stop the KPI worker on server shutdown.
	if kpiJob != nil {
		defer func() {
			if err := kpiJob.Stop(); err != nil {
				log.Printf("KPI refresh worker stop: %v", err)
			}
		}()
	}

	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeout) * time.Second,
	}

	pool, err := db.NewPool(context.Background(), cfg)
	if err != nil {
		log.Fatalf("failed to create database pool: %v", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)

	shutdownTimeout := time.Duration(cfg.GracefulShutdownTimeout) * time.Second
	if err := runHTTPServer(context.Background(), sig, srv, shutdownTimeout, func(ctx context.Context) error {
		if pool != nil {
			pool.Close()
		}
		return nil
	}); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func runHTTPServer(ctx context.Context, sig chan os.Signal, srv *http.Server, shutdownTimeout time.Duration, cleanup func(context.Context) error) error {
	start := time.Now()

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- listenAndServe(srv)
	}()

	select {
	case <-ctx.Done():
	case s := <-sig:
		log.Printf("received signal %v, initiating graceful shutdown", s)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		select {
		case <-sig:
			_ = srv.Close()
			return fmt.Errorf("forced shutdown after second signal: %w", err)
		default:
		}
		return fmt.Errorf("http server shutdown: %w", err)
	}

	if err := <-serveErr; err != nil && err != http.ErrServerClosed {
		return err
	}

	if cleanup != nil {
		if err := cleanup(context.Background()); err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
	}

	metrics.ShutdownDuration.Observe(time.Since(start).Seconds())
	return nil
}

func printConfigError(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
}

// stdLogger adapts the standard log package to the worker's logger interface.
type stdLogger struct{}

func (stdLogger) Error(msg string, keysAndValues ...any) {
	log.Println(append([]any{"ERROR: " + msg}, keysAndValues...)...)
}
