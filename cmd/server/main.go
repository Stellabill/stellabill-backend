package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/config"
	grpcserver "stellarbill-backend/internal/grpc"
	"stellarbill-backend/internal/handlers"
	"stellarbill-backend/internal/repository"
	"stellarbill-backend/internal/routes"
	"time"

	pb "stellarbill-backend/gen/stellabill/v1"

	"github.com/gin-gonic/gin"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	grpchealth "google.golang.org/grpc/health/grpc_health_v1"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/metrics"
)

// listenAndServe is a package-level variable so tests can inject a fake
// listener without network access.
var listenAndServe = func(srv *http.Server) error {
	return srv.ListenAndServe()
}

func main() {
	pool, srv, err := InitializeServer()
	if err != nil {
		printConfigError(err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTimeout := time.Duration(shutdownTimeoutSecs()) * time.Second

	// cleanup drains the database pool after the HTTP server has stopped
	// accepting new connections. It always runs (even if HTTP shutdown timed
	// out) so half-open connections are closed before the process exits.
	cleanup := func(cleanupCtx context.Context) error {
		start := time.Now()
		defer func() {
			metrics.ShutdownDuration.Observe(time.Since(start).Seconds())
		}()
		return db.DrainPool(cleanupCtx, pool)
	}

	log.Printf("server listening on %s", srv.Addr)
	if err := runHTTPServer(ctx, make(chan os.Signal, 1), srv, shutdownTimeout, cleanup); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// runHTTPServer starts srv and performs a graceful shutdown when ctx is
// cancelled.  The optional cleanup function is always called during shutdown
// (even if HTTP shutdown times out) so that resources such as the database
// pool are drained before the process exits.
// A second signal on secondSignal forces an immediate close.
func runHTTPServer(
	ctx context.Context,
	secondSignal <-chan os.Signal,
	srv *http.Server,
	shutdownTimeout time.Duration,
	cleanup func(context.Context) error,
) error {
	serverErr := make(chan error, 1)
	go func() {
		err := listenAndServe(srv)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErr <- err
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Always call cleanup on exit so resources (DB pool) are drained even
	// when the HTTP server shutdown times out.  cleanup gets a fresh context
	// to avoid racing with an expired shutdownCtx. Errors are logged rather
	// than returned so the HTTP shutdown error takes precedence.
	defer func() {
		if cleanup != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cleanupCancel()
			if err := cleanup(cleanupCtx); err != nil {
				log.Printf("cleanup error during shutdown: %v", err)
			}
		}
	}()

	// A second signal forces an immediate close.
	go func() {
		select {
		case <-secondSignal:
			srv.Close()
		case <-shutdownCtx.Done():
		}
	}()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}

	// Start gRPC server and grpc-gateway REST proxy on a separate port if configured
	if cfg.GRPCPort > 0 {
		grpcAddr := fmt.Sprintf(":%d", cfg.GRPCPort)
		gatewayPort := cfg.GRPCPort + 1 // gateway on next port
		startGRPCServer(cfg, grpcAddr, gatewayPort)
	}

	log.Printf("server listening on %s", addr)
	if err := listenAndServe(srv); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	if shutdownCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("http server shutdown: timed out")
	}

	if err := <-serverErr; err != nil {
		return err
	}

	return nil
}

// startGRPCServer starts the gRPC server and grpc-gateway REST proxy.
func startGRPCServer(cfg config.Config, grpcAddr string, gatewayPort int) {
	grpcCfg := grpcserver.ServerConfig{
		Port:       cfg.GRPCPort,
		CertFile:   cfg.GRPCCertFile,
		KeyFile:    cfg.GRPCKeyFile,
		CACertFile: cfg.GRPCCACertFile,
		EnableTLS:  cfg.GRPCEnableTLS,
	}

	// Use a simple token verifier based on the JWT secret.
	var verifier auth.TokenVerifier
	if cfg.JWTSecret != "" {
		verifier = auth.NewTokenGenerator(cfg.JWTSecret)
	}

	// Create the gRPC server
	grpcServer, err := grpcserver.NewServer(grpcCfg, verifier)
	if err != nil {
		log.Fatalf("failed to create gRPC server: %v", err)
	}

	// Create service dependencies and register gRPC services.
	subRepo := repository.NewMockSubscriptionRepo()
	planRepo := repository.NewMockPlanRepo()

	grpcServer.RegisterServices(
		grpcserver.NewPlanServiceServer(&noopPlanService{}),
		grpcserver.NewSubscriptionServiceServer(
			grpcserver.NewSubscriptionServiceWrapper(subRepo, planRepo),
		),
	)

	// Start the gRPC server
	lis, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("failed to listen on gRPC port %d: %v", cfg.GRPCPort, err)
	}

	go func() {
		log.Printf("gRPC server listening on %s", grpcAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("gRPC server error: %v", err)
		}
	}()

	// Start the grpc-gateway REST proxy
	go startGateway(gatewayPort, grpcAddr)
}

// startGateway starts the grpc-gateway HTTP reverse proxy.
// It translates REST JSON requests to gRPC and forwards them to the gRPC server.
func startGateway(gatewayPort int, grpcAddr string) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Create a gRPC client connection to the gRPC server
	conn, err := grpc.NewClient(
		grpcAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("failed to dial gRPC server for gateway: %v", err)
	}
	defer conn.Close()

	// Create the gateway mux
	gwmux := runtime.NewServeMux()

	// Register PlanService and SubscriptionService handlers
	if err := pb.RegisterPlanServiceHandler(ctx, gwmux, conn); err != nil {
		log.Fatalf("failed to register PlanService gateway: %v", err)
	}
	if err := pb.RegisterSubscriptionServiceHandler(ctx, gwmux, conn); err != nil {
		log.Fatalf("failed to register SubscriptionService gateway: %v", err)
	}

	// Register gRPC health check handler for the gateway
	if err := grpchealth.RegisterHealthHandler(ctx, gwmux, conn); err != nil {
		log.Printf("failed to register health gateway handler: %v", err)
	}

	gwAddr := fmt.Sprintf(":%d", gatewayPort)
	gwServer := &http.Server{
		Addr:    gwAddr,
		Handler: gwmux,
	}

	log.Printf("grpc-gateway REST proxy listening on %s (proxying to gRPC %s)", gwAddr, grpcAddr)
	if err := gwServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("grpc-gateway server error: %v", err)
	}
}

// noopPlanService is a minimal PlanService that returns empty results.
type noopPlanService struct{}

func (s *noopPlanService) ListPlans(c *gin.Context) ([]handlers.Plan, error) {
	return []handlers.Plan{}, nil
}

func printConfigError(err error) {
	fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
}

// stdLogger adapts the standard log package to the worker's logger interface.
type stdLogger struct{}

func (stdLogger) Error(msg string, keysAndValues ...any) {
	log.Println(append([]any{"ERROR: " + msg}, keysAndValues...)...)
}

// shutdownTimeoutSecs reads the GRACEFUL_SHUTDOWN_TIMEOUT env var, falling
// back to 30 seconds. This is kept outside config.Load() so main() can
// consume the graceful shutdown timeout without threading the full Config
// through InitializeServer.
func shutdownTimeoutSecs() int {
	if v := os.Getenv("GRACEFUL_SHUTDOWN_TIMEOUT"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 1 && s <= 600 {
			return s
		}
	}
	return 30
}
