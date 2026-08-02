package grpc

import (
	"crypto/tls"
	"fmt"
	"log"

	"stellarbill-backend/internal/auth"

	pb "stellarbill-backend/gen/stellabill/v1"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// ServerConfig holds configuration for the gRPC server.
type ServerConfig struct {
	Port       int
	CertFile   string
	KeyFile    string
	CACertFile string // Optional: for mTLS client verification
	EnableTLS  bool
}

// Server wraps the gRPC server with registered services.
type Server struct {
	*grpc.Server
	config ServerConfig
}

// NewServer creates a new gRPC server with the given auth verifier and config.
func NewServer(cfg ServerConfig, verifier auth.TokenVerifier) (*Server, error) {
	var opts []grpc.ServerOption

	// Add auth interceptor
	if verifier != nil {
		opts = append(opts, grpc.ChainUnaryInterceptor(
			AuthInterceptor(verifier),
		))
	}

	// Configure TLS/mTLS
	if cfg.EnableTLS {
		tlsConfig, err := loadTLSConfig(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS config: %w", err)
		}
		opts = append(opts, grpc.Creds(credentials.NewTLS(tlsConfig)))
	}

	grpcServer := grpc.NewServer(opts...)

	// Register health service
	healthServer := health.NewServer()
	healthpb.RegisterHealthServer(grpcServer, healthServer)
	healthServer.SetServingStatus("stellabill.internal.v1", healthpb.HealthCheckResponse_SERVING)

	return &Server{
		Server:  grpcServer,
		config:  cfg,
	}, nil
}

// RegisterServices registers the Plan and Subscription services on the gRPC server.
func (s *Server) RegisterServices(
	planServiceServer pb.PlanServiceServer,
	subscriptionServiceServer pb.SubscriptionServiceServer,
) {
	pb.RegisterPlanServiceServer(s.Server, planServiceServer)
	pb.RegisterSubscriptionServiceServer(s.Server, subscriptionServiceServer)
}

// loadTLSConfig creates a TLS configuration with optional mTLS.
func loadTLSConfig(cfg ServerConfig) (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server cert/key: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.CACertFile != "" {
		// mTLS: verify client certificates against CA
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		log.Printf("mTLS enabled with CA cert: %s", cfg.CACertFile)
	}

	return tlsConfig, nil
}
