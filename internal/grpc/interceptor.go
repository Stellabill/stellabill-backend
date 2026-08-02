package grpc

import (
	"context"
	"strings"

	"stellarbill-backend/internal/auth"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// AuthInterceptor returns a gRPC unary interceptor that validates JWT tokens
// by reusing the existing auth.TokenVerifier interface.
func AuthInterceptor(verifier auth.TokenVerifier) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		// Extract the authorization token from gRPC metadata.
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "missing metadata")
		}

		authHeaders := md.Get("authorization")
		if len(authHeaders) == 0 {
			return nil, status.Error(codes.Unauthenticated, "missing authorization header")
		}

		tokenString := strings.TrimPrefix(authHeaders[0], "Bearer ")
		if tokenString == authHeaders[0] && strings.HasPrefix(authHeaders[0], "Bearer ") {
			// The prefix existed but wasn't stripped — handle edge case
			tokenString = strings.TrimPrefix(authHeaders[0], "Bearer ")
		}

		if tokenString == "" || tokenString == authHeaders[0] {
			return nil, status.Error(codes.Unauthenticated, "invalid authorization format")
		}

		claims, err := verifier.Verify(ctx, tokenString)
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "token verification failed: %v", err)
		}

		// Store claims in context for downstream use.
		ctx = context.WithValue(ctx, auth.PrincipalKey, claims.UserID)

		if claims.TenantID != "" {
			ctx = context.WithValue(ctx, "tenant_id", claims.TenantID)
		}

		return handler(ctx, req)
	}
}

// NoopAuthInterceptor is an interceptor that passes through without auth.
// Used for health checks and testing.
func NoopAuthInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		return handler(ctx, req)
	}
}
