package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"database/sql"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"stellarbill-backend/internal/audit"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/routes"
	"stellarbill-backend/internal/security"
	"stellarbill-backend/internal/service"
	"stellarbill-backend/internal/shutdown"
	"stellarbill-backend/internal/startup"
	applogger "stellarbill-backend/internal/logger"
)
	"stellarbill-backend/internal/logger"
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

	// -------------------------------
	// LOGGER SETUP
	// -------------------------------
	var logger *zap.Logger
	if cfg.Env == "production" {
		logger = security.ProductionLogger()
		gin.SetMode(gin.ReleaseMode)
	} else {
		logger = security.DevLogger()
		gin.SetMode(gin.DebugMode)
	}
	defer logger.Sync()

	// -------------------------------
	// ROUTER SETUP
	// -------------------------------
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.RequestLogger())

	// Security headers
	router.Use(func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Next()
	})

	// Wire up services and handlers, then register routes
	planSvc := service.NewPlanService()
	subSvc := service.NewSubscriptionService()
	h := handlers.NewHandler(planSvc, subSvc)
	routes.Register(router, h)

	// -------------------------------
	// DATABASE (if exists)
	// -------------------------------
	var dbConn *sql.DB // replace with real DB if available

	txManager := db.NewTxManager(dbConn)

	// -------------------------------
	// HTTP SERVER
	// -------------------------------
	addr := fmt.Sprintf(":%d", cfg.Port)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  time.Duration(cfg.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(cfg.IdleTimeout) * time.Second,
	}

	// -------------------------------
	// GRACEFUL SHUTDOWN
	// -------------------------------
	gs := shutdown.NewGracefulShutdown(
		srv,
		30*time.Second,
		20*time.Second,
	)

	// 🔥 CRITICAL: propagate shutdown context
	srv.BaseContext = func(_ net.Listener) context.Context {
		return gs.Context()
	}

	// -------------------------------
	// CLEANUP CALLBACKS
	// -------------------------------

	// DB safety
	gs.OnShutdown(func(ctx context.Context) error {
		log.Println("Waiting for DB transactions...")
		return txManager.Wait(ctx)
	})

	// Audit logs
	gs.RegisterAuditFlush(func(ctx context.Context) error {
		log.Println("Flushing audit logs...")
		time.Sleep(1 * time.Second)
		return nil
	})

	// Outbox events
	gs.RegisterOutboxFlush(func(ctx context.Context) error {
		log.Println("Flushing outbox events...")
		time.Sleep(1 * time.Second)
		return nil
	})

	// -------------------------------
	// START SERVER
	// -------------------------------
	go func() {
		logger.Info("Server starting", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
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

	applogger.Init()

	r := gin.New()

	// Order matters: RequestID first so the hardened Recovery middleware can
	// always include a correlation id in the log and the error envelope.
	r.Use(middleware.RequestID())
	r.Use(middleware.Recovery())
	r.Use(middleware.RequestLogger())

	var db *sql.DB = nil // existing or future DB

	logger.Info("Server exited cleanly")
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
