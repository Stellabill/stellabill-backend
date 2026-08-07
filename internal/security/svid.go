package security

import (
	"context"
	"crypto/tls"
	"fmt"
	"sync"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
)

// SVIDRotator holds a rotating X.509 SVID fetched from the SPIRE workload API.
// Certificate rotation is transparent to in-flight connections — existing
// connections continue with the cert they were established with; new dials
// pick up the rotated identity automatically.
type SVIDRotator struct {
	mu         sync.RWMutex
	source     *workloadapi.X509Source
	socketPath string
}

// NewSVIDRotator creates a new rotator connected to the SPIRE workload API
// at the given socket path (e.g. "unix:///run/spire/sockets/agent.sock").
func NewSVIDRotator(ctx context.Context, socketPath string) (*SVIDRotator, error) {
	source, err := workloadapi.NewX509Source(ctx,
		workloadapi.WithClientOptions(workloadapi.WithAddr(socketPath)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create X509 source: %w", err)
	}

	return &SVIDRotator{
		source:     source,
		socketPath: socketPath,
	}, nil
}

// ServerTLSConfig returns a tls.Config for the gRPC server that:
// - Requires client certificates (mTLS)
// - Accepts only clients presenting a SPIFFE ID in the allowed set
// - Automatically uses the latest SVID on each new connection
func (r *SVIDRotator) ServerTLSConfig(allowedIDs ...spiffeid.ID) *tls.Config {
	return tlsconfig.MTLSServerConfig(r.source, r.source, tlsconfig.AuthorizeOneOf(allowedIDs...))
}

// ClientTLSConfig returns a tls.Config for gRPC client dials that:
// - Presents the workload's SVID to the server
// - Verifies the server presents the expected SPIFFE ID
// - Uses the latest SVID on each new dial (rotation transparent)
func (r *SVIDRotator) ClientTLSConfig(serverID spiffeid.ID) *tls.Config {
	return tlsconfig.MTLSClientConfig(r.source, r.source, tlsconfig.AuthorizeID(serverID))
}

// Close shuts down the SVID source and stops background rotation.
func (r *SVIDRotator) Close() error {
	return r.source.Close()
}

// GetCurrentSVIDExpiry returns the expiry time of the current SVID.
// Useful for health checks and monitoring.
func (r *SVIDRotator) GetCurrentSVIDExpiry() (time.Time, error) {
	svid, err := r.source.GetX509SVID()
	if err != nil {
		return time.Time{}, fmt.Errorf("failed to get current SVID: %w", err)
	}

	if len(svid.Certificates) == 0 {
		return time.Time{}, fmt.Errorf("SVID has no certificates")
	}

	return svid.Certificates[0].NotAfter, nil
}
