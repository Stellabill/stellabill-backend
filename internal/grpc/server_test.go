package grpc

import (
	"context"
	"net"
	"testing"
	"time"

	"stellarbill-backend/internal/auth"
	"stellarbill-backend/internal/handlers"

	"github.com/gin-gonic/gin"

	pb "stellarbill-backend/gen/stellabill/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// ── Stub service implementations ──────────────────────────────────────────────

type stubPlanService struct {
	plans []handlers.Plan
	err   error
}

func (s *stubPlanService) ListPlans(_ *gin.Context) ([]handlers.Plan, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.plans == nil {
		return []handlers.Plan{}, nil
	}
	return s.plans, nil
}

type stubSubscriptionService struct {
	subs []handlers.Subscription
	err  error
}

func (s *stubSubscriptionService) ListSubscriptions(_ *gin.Context) ([]handlers.Subscription, error) {
	if s.err != nil {
		return nil, s.err
	}
	if s.subs == nil {
		return []handlers.Subscription{}, nil
	}
	return s.subs, nil
}

func (s *stubSubscriptionService) GetSubscription(_ *gin.Context, id string) (*handlers.Subscription, error) {
	if s.err != nil {
		return nil, s.err
	}
	for i := range s.subs {
		if s.subs[i].ID == id {
			return &s.subs[i], nil
		}
	}
	return nil, assert.AnError
}

// ── Token verifier stubs ──────────────────────────────────────────────────────

type stubTokenVerifier struct {
	claims *auth.Claims
	err    error
}

func (s *stubTokenVerifier) Verify(_ context.Context, _ string) (*auth.Claims, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.claims, nil
}

// ── Test helpers ──────────────────────────────────────────────────────────────

func startTestServer(t *testing.T, verifier auth.TokenVerifier) (pb.PlanServiceClient, pb.SubscriptionServiceClient, grpc_health_v1.HealthClient, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	srv, err := NewServer(ServerConfig{}, verifier)
	require.NoError(t, err)

	srv.RegisterServices(
		NewPlanServiceServer(&stubPlanService{
			plans: []handlers.Plan{
				{ID: "plan-1", Name: "Basic", Amount: "1000", Currency: "USD", Interval: "month"},
				{ID: "plan-2", Name: "Pro", Amount: "2000", Currency: "USD", Interval: "month"},
			},
		}),
		NewSubscriptionServiceServer(&stubSubscriptionService{
			subs: []handlers.Subscription{
				{ID: "sub-1", PlanID: "plan-1", Customer: "cust-1", Status: "active", Amount: "1000", Interval: "month"},
			},
		}),
	)

	go func() {
		_ = srv.Serve(lis)
	}()

	addr := lis.Addr().String()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	planClient := pb.NewPlanServiceClient(conn)
	subClient := pb.NewSubscriptionServiceClient(conn)
	healthClient := grpc_health_v1.NewHealthClient(conn)

	cleanup := func() {
		conn.Close()
		srv.GracefulStop()
	}

	return planClient, subClient, healthClient, cleanup
}

func authContext(_ *stubTokenVerifier) context.Context {
	return metadata.AppendToOutgoingContext(
		context.Background(),
		"authorization", "Bearer test-token",
	)
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestGRPCServer_HealthCheck(t *testing.T) {
	_, _, healthClient, cleanup := startTestServer(t, nil)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := healthClient.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: "stellabill.internal.v1"})
	require.NoError(t, err)
	assert.Equal(t, grpc_health_v1.HealthCheckResponse_SERVING, resp.Status)
}

func TestGRPCServer_PlanService_WithAuth(t *testing.T) {
	planClient, _, _, cleanup := startTestServer(t, &stubTokenVerifier{
		claims: &auth.Claims{UserID: "test-user", Role: auth.RoleAdmin},
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(authContext(nil), 5*time.Second)
	defer cancel()

	resp, err := planClient.ListPlans(ctx, &pb.ListPlansRequest{Limit: 10})
	require.NoError(t, err)
	require.Len(t, resp.Plans, 2)
	assert.Equal(t, "plan-1", resp.Plans[0].Id)
}

func TestGRPCServer_SubscriptionService_WithAuth(t *testing.T) {
	_, subClient, _, cleanup := startTestServer(t, &stubTokenVerifier{
		claims: &auth.Claims{UserID: "test-user", Role: auth.RoleAdmin},
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(authContext(nil), 5*time.Second)
	defer cancel()

	resp, err := subClient.GetSubscription(ctx, &pb.GetSubscriptionRequest{Id: "sub-1"})
	require.NoError(t, err)
	require.NotNil(t, resp.Subscription)
	assert.Equal(t, "sub-1", resp.Subscription.Id)
}

func TestGRPCServer_Unauthenticated(t *testing.T) {
	planClient, _, _, cleanup := startTestServer(t, &stubTokenVerifier{
		err: assert.AnError,
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := planClient.ListPlans(ctx, &pb.ListPlansRequest{Limit: 10})
	require.Error(t, err)
}

func TestGRPCServer_NoAuthHeader(t *testing.T) {
	planClient, _, _, cleanup := startTestServer(t, &stubTokenVerifier{
		claims: &auth.Claims{UserID: "test-user"},
	})
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := planClient.ListPlans(ctx, &pb.ListPlansRequest{Limit: 10})
	require.Error(t, err)
}

func TestGRPCServer_NewServer_NilVerifier(t *testing.T) {
	srv, err := NewServer(ServerConfig{}, nil)
	require.NoError(t, err)
	require.NotNil(t, srv)
}

func TestNewServer_WithTLS_InvalidCert(t *testing.T) {
	_, err := NewServer(ServerConfig{
		EnableTLS: true,
		CertFile:  "/nonexistent/cert.pem",
		KeyFile:   "/nonexistent/key.pem",
	}, nil)
	require.Error(t, err)
}

func TestNewServer_RegisterServices(t *testing.T) {
	srv, err := NewServer(ServerConfig{}, nil)
	require.NoError(t, err)

	planSrv := NewPlanServiceServer(&stubPlanService{})
	subSrv := NewSubscriptionServiceServer(&stubSubscriptionService{})

	// Should not panic
	srv.RegisterServices(planSrv, subSrv)
}

func TestAuthInterceptor_MissingMetadata(t *testing.T) {
	interceptor := AuthInterceptor(&stubTokenVerifier{})
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	_, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, handler)
	require.Error(t, err)
}

func TestAuthInterceptor_InvalidToken(t *testing.T) {
	interceptor := AuthInterceptor(&stubTokenVerifier{err: assert.AnError})
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer invalid-token")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	require.Error(t, err)
}

func TestNoopAuthInterceptor(t *testing.T) {
	interceptor := NoopAuthInterceptor()
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	resp, err := interceptor(context.Background(), nil, &grpc.UnaryServerInfo{}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
}

func TestAuthInterceptor_EmptyBearerToken(t *testing.T) {
	interceptor := AuthInterceptor(&stubTokenVerifier{})
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer ")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	require.Error(t, err)
}

func TestAuthInterceptor_NoBearerPrefix(t *testing.T) {
	interceptor := AuthInterceptor(&stubTokenVerifier{})
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "not-bearer-token")
	_, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	require.Error(t, err)
}

func TestAuthInterceptor_SuccessfulAuth(t *testing.T) {
	interceptor := AuthInterceptor(&stubTokenVerifier{
		claims: &auth.Claims{UserID: "user-1", TenantID: "tenant-1"},
	})
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		// Verify claims are in context
		principal, ok := ctx.Value(auth.PrincipalKey).(string)
		require.True(t, ok)
		assert.Equal(t, "user-1", principal)

		tenantID, ok := ctx.Value("tenant_id").(string)
		require.True(t, ok)
		assert.Equal(t, "tenant-1", tenantID)

		return "ok", nil
	}

	md := metadata.Pairs("authorization", "Bearer my-token")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	resp, err := interceptor(ctx, nil, &grpc.UnaryServerInfo{}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
}

func TestAuthInterceptor_LoadTLSConfig(t *testing.T) {
	// Test the helper directly for coverage
	cfg := ServerConfig{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
	}
	_, err := loadTLSConfig(cfg)
	require.Error(t, err)
}
