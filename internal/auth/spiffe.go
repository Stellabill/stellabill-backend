package auth

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// spiffeVerifier validates JWT-SVIDs against the SPIFFE Workload API.
type spiffeVerifier struct {
	source      *workloadapi.JWTSource
	trustDomain spiffeid.TrustDomain
	socketPath  string
	disabled    bool
}

// NewSpiffeVerifier initializes a new SPIFFE verifier connected to the Workload API.
// It gracefully degrades to disabled if the socket is not available in non-prod.
func NewSpiffeVerifier(ctx context.Context, socketPath, trustDomainStr string, env string) (TokenVerifier, error) {
	if socketPath == "" {
		if env != "production" {
			log.Println("SPIFFE socket path not provided, disabling SPIFFE verifier in dev")
			return &spiffeVerifier{disabled: true}, nil
		}
		return nil, errors.New("SPIFFE_ENDPOINT_SOCKET is required in production")
	}

	td, err := spiffeid.TrustDomainFromString(trustDomainStr)
	if err != nil {
		return nil, fmt.Errorf("invalid trust domain: %w", err)
	}

	clientOptions := []workloadapi.JWTSourceOption{
		workloadapi.WithClientOptions(workloadapi.WithAddr("unix://" + socketPath)),
	}

	source, err := workloadapi.NewJWTSource(ctx, clientOptions...)
	if err != nil {
		if env != "production" {
			log.Printf("Failed to connect to Workload API, disabling SPIFFE: %v", err)
			return &spiffeVerifier{disabled: true}, nil
		}
		return nil, fmt.Errorf("failed to create JWT source: %w", err)
	}

	return &spiffeVerifier{
		source:      source,
		trustDomain: td,
		socketPath:  socketPath,
	}, nil
}

// Verify implements TokenVerifier to validate the JWT-SVID.
func (v *spiffeVerifier) Verify(ctx context.Context, tokenString string) (*Claims, error) {
	if v.disabled {
		return nil, errors.New("SPIFFE verifier is disabled")
	}

	svid, err := workloadapi.ParseJWTSVID(ctx, tokenString, v.source, []string{v.trustDomain.IDString()})
	if err != nil {
		return nil, fmt.Errorf("failed to parse or validate JWT-SVID: %w", err)
	}

	if svid.ID.TrustDomain() != v.trustDomain {
		return nil, errors.New("trust domain mismatch")
	}

	// Map SPIFFE SVID to internal Claims.
	claims := &Claims{
		UserID: svid.ID.String(),
		Role:   RoleAdmin, // Default for internal mesh traffic
	}

	return claims, nil
}

// FetchJWTSVID fetches a JWT-SVID for outbound internal API calls.
func (v *spiffeVerifier) FetchJWTSVID(ctx context.Context, audience string) (string, error) {
	if v.disabled {
		return "", errors.New("SPIFFE is disabled")
	}
	svid, err := v.source.FetchJWTSVID(ctx, []string{audience})
	if err != nil {
		return "", fmt.Errorf("failed to fetch JWT-SVID: %w", err)
	}
	return svid.Marshal(), nil
}

// Close cleans up the Workload API source.
func (v *spiffeVerifier) Close() {
	if v.source != nil {
		v.source.Close()
	}
}
