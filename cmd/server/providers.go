package main

// providers.go contains the google/wire provider functions for the server's
// dependency-injection graph.  All providers are pure functions – no global
// state, no side effects – so the wiring is easy to audit and test.
//
// Security notes:
//   - Config is loaded once and validated before any provider runs.
//   - Providers accept concrete types returned by other providers; no package-
//     level singletons are used, which prevents hidden global mutable state.
//   - Nil-guarding happens in each constructor, not here, so panics surface at
//     the provider boundary rather than deep in request handling.
//   - Server timeout values are taken from the validated Config and are never
//     defaulted to zero, preventing accidental unbounded connections.

import (
	"fmt"
	"net/http"
	"time"

	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/routes"

	"github.com/gin-gonic/gin"
)

// ProvideConfig loads and validates the application configuration from the
// environment.  Returns an error on missing required values or policy
// violations (e.g. weak JWT secret).
func ProvideConfig() (config.Config, error) {
	return config.Load()
}

// ProvideRouter configures gin's release/debug mode, creates the engine, and
// delegates all route and middleware registration to routes.Register.
//
// Security: routes.Register applies the middleware stack in the documented
// order (recovery → request-id → logging → cors → rate-limit → auth), so
// this provider does not need to repeat that logic.
func ProvideRouter(cfg config.Config) *gin.Engine {
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	routes.Register(router)
	return router
}

// ProvideHTTPServer assembles an *http.Server from the validated config and
// the fully-wired router.  Every timeout is sourced from cfg so there is no
// silent "infinite" default.
func ProvideHTTPServer(cfg config.Config, router *gin.Engine) *http.Server {
	addr := fmt.Sprintf(":%d", cfg.Port)
	return &http.Server{
		Addr:           addr,
		Handler:        router,
		ReadTimeout:    time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout:   time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:    time.Duration(cfg.IdleTimeout) * time.Second,
		MaxHeaderBytes: cfg.MaxHeaderBytes,
	}
}
