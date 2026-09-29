package security

import (
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc/credentials"
)

// ServerCredentials returns gRPC transport credentials for the gRPC server.
// Enforces mTLS — clients without a valid SPIFFE cert are rejected.
func (r *SVIDRotator) ServerCredentials(allowedClientIDs ...spiffeid.ID) credentials.TransportCredentials {
	return credentials.NewTLS(r.ServerTLSConfig(allowedClientIDs...))
}

// ClientCredentials returns gRPC transport credentials for dialing the server.
// Presents the workload SVID and verifies the server's SPIFFE ID.
func (r *SVIDRotator) ClientCredentials(serverID spiffeid.ID) credentials.TransportCredentials {
	return credentials.NewTLS(r.ClientTLSConfig(serverID))
}
