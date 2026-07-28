package auth

import (
	"context"
)

// TokenVerifier defines the interface for verifying authentication tokens.
type TokenVerifier interface {
	Verify(ctx context.Context, tokenString string) (*Claims, error)
}
