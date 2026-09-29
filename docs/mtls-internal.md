# Internal mTLS Between API and Worker Pods

## Overview

All internal gRPC calls between API pods and worker pods are protected by mutual TLS (mTLS) using SPIFFE/SPIRE-issued short-lived X.509 certificates. This design eliminates the shared-secret coupling between API and worker, replacing it with cryptographic identity.

## Architecture

- **SPIRE Agent**: Runs as a sidecar in each pod and manages the workload's identity
- **SPIRE Server**: Central authority that issues short-lived X.509 SVIDs (Signed Workload Identity Documents)
- **Workload API**: UNIX socket interface (`/run/spire/sockets/agent.sock`) that provides SVIDs to the application
- **X509Source**: go-spiffe library component that fetches and caches SVIDs from the workload API
- **SVIDRotator**: Stellabill component that wraps X509Source and provides gRPC TLS configs

### Certificate Lifecycle

1. **Issuance**: SPIRE server issues X.509 certificate with embedded SPIFFE ID (Subject Alternative Name)
2. **Delivery**: SPIRE agent provides certificate via workload API socket
3. **Caching**: X509Source watches the socket and keeps certificate in memory
4. **Rotation**: When TTL expires (~50% of actual TTL), SPIRE agent fetches a new cert from the server
5. **Consumption**: Applications retrieve the current cert via the TLS config callback

### Connection Lifecycle

- **Existing connections**: Continue using the certificate they were established with
  - No disruption or restart required on rotation
  - In-flight gRPC streams/RPCs complete normally
- **New dials**: Automatically pick up the rotated certificate
  - Each `grpc.NewClient()` or `grpc.Dial()` invocation gets the latest SVID
  - Rotation is completely transparent

## SPIFFE IDs

| Workload | SPIFFE ID |
|----------|-----------|
| API pods | `spiffe://stellabill.internal/api` |
| Worker pods | `spiffe://stellabill.internal/worker` |

These IDs are used for:
- **Server authorization**: API server accepts connections from pods with `spiffe://stellabill.internal/worker` SPIFFE ID
- **Client verification**: Worker clients verify that the server presents `spiffe://stellabill.internal/api` in its certificate
- **Audit trail**: Each RPC is authenticated with the client's SPIFFE ID (available in peer certificate)

## Environment Variables

Configuration is managed via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `SPIRE_SOCKET_PATH` | `unix:///run/spire/sockets/agent.sock` | SPIRE agent workload API socket path |
| `API_SPIFFE_ID` | `spiffe://stellabill.internal/api` | Expected SPIFFE ID of API pods |
| `WORKER_SPIFFE_ID` | `spiffe://stellabill.internal/worker` | Expected SPIFFE ID of worker pods |

## Certificate Rotation Cadence

- **SVID TTL**: Configurable in SPIRE server (recommended: 1 hour)
- **Rotation trigger**: At ~50% of TTL (approximately 30 minutes before expiry)
- **Delivery mechanism**: SPIRE agent watches and updates the workload API socket
- **Application behavior**: go-spiffe X509Source automatically detects changes and updates in-memory certificate
- **Connection impact**: Existing connections unaffected; new dials use rotated cert

### Example TTL Scenario

```
Time          Event
0:00          Certificate issued, TTL = 1 hour, expires at 1:00
0:30          SPIRE agent triggers rotation at ~50% of TTL
0:30-0:31     New cert issued and cached by X509Source
0:30-1:00     Old connections continue; new dials use new cert
1:00          Old cert would expire (but already replaced)
```

## Implementation

### API Server (gRPC Server)

The API pod acts as a gRPC server accepting connections from worker pods.

```go
import (
    "context"
    "stellarbill-backend/internal/security"
    "github.com/spiffe/go-spiffe/v2/spiffeid"
    "google.golang.org/grpc"
)

// In cmd/server/main.go or similar:
ctx := context.Background()

// Create SVID rotator connected to SPIRE workload API
rotator, err := security.NewSVIDRotator(ctx, cfg.SpireSocketPath)
if err != nil {
    log.Fatalf("failed to init SVID rotator: %v", err)
}
defer rotator.Close()

// Parse the expected worker SPIFFE ID
workerID, err := spiffeid.IDFromString(cfg.WorkerSpiffeID)
if err != nil {
    log.Fatalf("invalid worker SPIFFE ID: %v", err)
}

// Create gRPC server with mTLS credentials
// This server:
// - Presents the API pod's SVID
// - Requires client certificates
// - Only accepts connections from pods with the worker SPIFFE ID
grpcServer := grpc.NewServer(
    grpc.Creds(rotator.ServerCredentials(workerID)),
)

// Register your services
pb.RegisterYourServiceServer(grpcServer, &yourServiceImpl{})

// Listen and serve
listener, err := net.Listen("tcp", ":50051")
if err != nil {
    log.Fatalf("failed to listen: %v", err)
}

if err := grpcServer.Serve(listener); err != nil {
    log.Fatalf("server error: %v", err)
}
```

### Worker Client (gRPC Client)

The worker pod acts as a gRPC client dialing the API pod.

```go
import (
    "context"
    "stellarbill-backend/internal/security"
    "github.com/spiffe/go-spiffe/v2/spiffeid"
    "google.golang.org/grpc"
)

// In internal/worker or similar:
ctx := context.Background()

// Create SVID rotator connected to SPIRE workload API
rotator, err := security.NewSVIDRotator(ctx, cfg.SpireSocketPath)
if err != nil {
    return fmt.Errorf("failed to init SVID rotator: %w", err)
}
defer rotator.Close()

// Parse the expected API SPIFFE ID
apiID, err := spiffeid.IDFromString(cfg.APISpiffeID)
if err != nil {
    return fmt.Errorf("invalid API SPIFFE ID: %w", err)
}

// Dial the API server with mTLS credentials
// This client:
// - Presents the worker pod's SVID
// - Verifies the server presents the API SPIFFE ID
// - Automatically uses rotated certs on new dials
conn, err := grpc.NewClient(
    "api-service:50051",
    grpc.WithTransportCredentials(rotator.ClientCredentials(apiID)),
)
if err != nil {
    return fmt.Errorf("failed to dial: %w", err)
}
defer conn.Close()

// Use the connection
client := pb.NewYourServiceClient(conn)
resp, err := client.SomeRPC(ctx, &pb.Request{})
```

## Kubernetes Deployment

### SPIRE Configuration

Before deploying, ensure SPIRE is configured with registration entries for both workloads:

```bash
# Register API pod workload
spire-server entry create \
  -spiffeID spiffe://stellabill.internal/api \
  -parentID spiffe://stellabill.internal/sa/api \
  -selector k8s:ns:default \
  -selector k8s:pod-label:app:stellabill-api

# Register worker pod workload
spire-server entry create \
  -spiffeID spiffe://stellabill.internal/worker \
  -parentID spiffe://stellabill.internal/sa/worker \
  -selector k8s:ns:default \
  -selector k8s:pod-label:app:stellabill-worker
```

### Pod Configuration

Ensure both API and worker pods have:

1. **SPIRE agent sidecar**: Running and healthy
2. **Socket mount**: `/run/spire/sockets/` mounted from SPIRE agent
3. **Environment variables**: Set the configuration variables (see above)
4. **Service account**: Correct labels for SPIRE registration entry matching

Example pod spec:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: stellabill-api
spec:
  serviceAccountName: stellabill-api
  containers:
  - name: api
    image: stellabill:latest
    env:
    - name: SPIRE_SOCKET_PATH
      value: "unix:///run/spire/sockets/agent.sock"
    - name: API_SPIFFE_ID
      value: "spiffe://stellabill.internal/api"
    - name: WORKER_SPIFFE_ID
      value: "spiffe://stellabill.internal/worker"
    volumeMounts:
    - name: spire-agent-socket
      mountPath: /run/spire/sockets
      readOnly: true
  - name: spire-agent
    image: ghcr.io/spiffe/spire-agent:latest
    volumeMounts:
    - name: spire-agent-socket
      mountPath: /run/spire/sockets
  volumes:
  - name: spire-agent-socket
    emptyDir: {}
```

## Local Development

For local development without a full SPIRE deployment:

### Option 1: Skip mTLS in Development

Set environment variables to disable mTLS:

```bash
# In development environment
export SPIRE_SOCKET_PATH=""  # Empty disables SPIRE connection
```

Then conditionally create the rotator:

```go
var rotator *security.SVIDRotator
if cfg.SpireSocketPath != "" {
    rotator, err = security.NewSVIDRotator(ctx, cfg.SpireSocketPath)
    if err != nil {
        return err
    }
    defer rotator.Close()
}

// Create server with mTLS only if rotator is available
var serverOpts []grpc.ServerOption
if rotator != nil {
    serverOpts = append(serverOpts, grpc.Creds(rotator.ServerCredentials(workerID)))
}
grpcServer := grpc.NewServer(serverOpts...)
```

### Option 2: Run SPIRE Locally

```bash
# Start SPIRE server and agent in containers
docker run -d \
  -p 8081:8081 \
  -p 8085:8085 \
  ghcr.io/spiffe/spire-server:latest

docker run -d \
  -v /tmp/spire:/tmp/spire \
  ghcr.io/spiffe/spire-agent:latest
```

Then set `SPIRE_SOCKET_PATH` to point to the agent socket.

### Option 3: Test Mode

For integration tests, use a mock X509Source or test helper:

```go
// In internal/security/testhelper_test.go
func NewTestSVIDRotator(t *testing.T) *SVIDRotator {
    // Return a rotator with mock or real test certs
    // This would depend on test infrastructure
}
```

## Monitoring and Observability

### Metrics

Monitor SVID rotation health:

```go
// Hook into SVIDRotator.GetCurrentSVIDExpiry() for monitoring
expiry, err := rotator.GetCurrentSVIDExpiry()
if err != nil {
    // Log warning: SVID retrieval failed
}

ttl := time.Until(expiry)
if ttl < 5*time.Minute {
    // Log warning: SVID expiring soon
}

// Expose as Prometheus gauge
svidExpiryGauge.Set(float64(expiry.Unix()))
```

### Audit Logging

gRPC peer information includes the client's certificate. Extract the SPIFFE ID:

```go
import "google.golang.org/grpc/peer"

// In your gRPC service handler:
p, ok := peer.FromContext(ctx)
if ok && p.AuthInfo != nil {
    tlsAuth := p.AuthInfo.(credentials.TLSInfo)
    // Extract SPIFFE ID from tlsAuth.State.PeerCertificates[0]
    // Log for audit trail
}
```

## Troubleshooting

### Connection Refused / Dial Failures

**Symptom**: `connection refused` when worker tries to dial API

**Causes**:
- SPIRE agent not running in pod
- Workload API socket not mounted correctly
- SPIRE server registration entry missing or misconfigured
- Firewall/NetworkPolicy blocking connection

**Diagnosis**:
```bash
# Check SPIRE agent is running
kubectl logs <pod> -c spire-agent

# Check socket is mounted
kubectl exec <pod> -- ls -la /run/spire/sockets/agent.sock

# Verify SPIRE registration entry
spire-server entry list
```

### Certificate Verification Failed

**Symptom**: `certificate verify failed` or `CERTIFICATE_VERIFY_FAILED`

**Causes**:
- Server SPIFFE ID doesn't match expected ID
- Client sending wrong SPIFFE ID
- SPIRE registration entry has wrong SPIFFE ID

**Diagnosis**:
```bash
# Check peer certificate in gRPC trace
grpcurl -v <server>:<port> list

# Verify SPIFFE IDs match configuration
echo $API_SPIFFE_ID $WORKER_SPIFFE_ID
```

### Rotation Issues

**Symptom**: New connections fail after ~50 minutes (typical rotation time)

**Causes**:
- SPIRE agent rotation failed
- X509Source not updating on socket change
- Certificate TTL misconfigured

**Diagnosis**:
```bash
# Check SPIRE agent logs for rotation events
kubectl logs <pod> -c spire-agent | grep rotation

# Monitor certificate expiry
kubectl exec <pod> -c api -- curl http://localhost:8080/metrics | grep svid_expiry
```

## Security Considerations

### Trust Domain

- **Trust Domain**: `stellabill.internal` (internal namespace)
- **Not a real domain**: Used only within Kubernetes cluster
- **Isolation**: SPIRE instances for different environments should use different trust domains

### Key Management

- **Private keys**: Never leave the SPIRE agent; never visible to application
- **Public certs**: Embedded in certificate (SPIFFE ID in SAN extension)
- **Rotation**: Automatic, transparent to application
- **No manual rotation**: Don't copy or manage certs manually

### Least Privilege

- **API → Worker**: No enforcement; API doesn't need to dial workers
  - If needed in future, use another SPIFFE ID like `spiffe://stellabill.internal/worker-callback`
- **Worker → API**: Enforced; worker only dials API
- **Fiber pods**: Can be added with new registration entries and SPIFFE IDs as needed

## References

- [SPIFFE Specification](https://github.com/spiffe/spiffe/blob/main/specification.md)
- [go-spiffe Documentation](https://pkg.go.dev/github.com/spiffe/go-spiffe/v2)
- [SPIRE Documentation](https://spiffe.io/docs/latest/deploying-spire/)
- [gRPC Security](https://grpc.io/docs/guides/auth/)
- [Kubernetes SPIRE Integration](https://spiffe.io/docs/latest/try-spiffe/getting-started-linux-macos-windows/)
