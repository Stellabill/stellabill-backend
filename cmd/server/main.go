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

var listenAndServe = func(srv *http.Server) error {
	return srv.ListenAndServe()
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		printConfigError(err)
		os.Exit(1)
	}

	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
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

func printConfigError(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
}
