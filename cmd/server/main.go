package main

import (
	"context"
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

	// Start gRPC server and grpc-gateway REST proxy on a separate port if configured
	if cfg.GRPCPort > 0 {
		grpcAddr := fmt.Sprintf(":%d", cfg.GRPCPort)
		gatewayPort := cfg.GRPCPort + 1 // gateway on next port
		startGRPCServer(cfg, grpcAddr, gatewayPort)
	}

	log.Printf("server listening on %s", addr)
	if err := listenAndServe(srv); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
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
	fmt.Fprintf(os.Stderr, "%v\n", err)
}
