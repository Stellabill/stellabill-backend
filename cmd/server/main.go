package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/routes"
	"stellarbill-backend/internal/worker"
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

	// Database connection — required for KPI metrics and other background jobs.
	var db *sql.DB
	if cfg.DBConn != "" {
		db, err = openDB("postgres", cfg.DBConn)
		if err != nil {
			log.Fatalf("failed to open database: %v", err)
		}
		defer db.Close()
		db.SetMaxOpenConns(cfg.DBPoolMaxConns)
		db.SetMaxIdleConns(cfg.DBPoolMinConns)
		db.SetConnMaxLifetime(time.Duration(cfg.DBPoolMaxConnLifetime) * time.Second)
		db.SetConnMaxIdleTime(time.Duration(cfg.DBPoolMaxConnIdleTime) * time.Second)
		log.Println("database connection established")
	}

	// Initialize SPIFFE Verifier for cross-service mesh auth
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

	log.Printf("server listening on %s", addr)
	if err := listenAndServe(srv); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func printConfigError(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
}

// stdLogger adapts the standard log package to the worker's logger interface.
type stdLogger struct{}

func (stdLogger) Error(msg string, keysAndValues ...any) {
	log.Println(append([]any{"ERROR: " + msg}, keysAndValues...)...)
}
