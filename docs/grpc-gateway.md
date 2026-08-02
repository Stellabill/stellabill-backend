# gRPC Gateway for Internal Service-to-Service Calls

## Overview

This feature adds a gRPC server alongside the existing REST API to enable
lower-latency internal service-to-service reads. The same protobuf definitions
are used to generate:

- **gRPC stubs** — for direct gRPC calls (binary, lower latency)
- **grpc-gateway REST handlers** — auto-generated REST endpoints from the proto
  definitions, ensuring parity between REST and gRPC responses

The public REST surface remains unchanged. The gRPC server runs on a *separate*
port so there is no conflict with the existing Gin-based REST handlers.

## Architecture

```
                     Internal Services
                           │
              ┌────────────┴────────────┐
              ▼                         ▼
      ┌──────────────┐       ┌──────────────────┐
      │  Gin REST    │       │  grpc-gateway     │
      │  (port 8080) │       │  (port GRPC_PORT  │
      │              │       │   + 1)            │
      └──────────────┘       └────────┬─────────┘
                                      │ HTTP/JSON
                                      ▼
                              ┌──────────────┐
                              │  gRPC Server  │
                              │  (GRPC_PORT)  │
                              │              │
                              │ Plans        │
                              │ Subscriptions│
                              └──────────────┘
```

- **Public REST** (port 8080): Unchanged, served by Gin handlers
- **gRPC Server** (GRPC_PORT): Binary protocol, auth via JWT/SPIFFE interceptor
- **grpc-gateway** (GRPC_PORT + 1): REST JSON proxy → gRPC, same proto-derived
  responses as the gRPC endpoints

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `GRPC_PORT`         | `0`     | gRPC server port. `0` = disabled |
| `GRPC_CERT_FILE`    | `""`    | Path to TLS certificate for gRPC |
| `GRPC_KEY_FILE`     | `""`    | Path to TLS private key for gRPC |
| `GRPC_CA_CERT_FILE` | `""`    | Path to CA cert for mTLS (optional) |
| `GRPC_ENABLE_TLS`   | `false` | Enable TLS for gRPC |

When `GRPC_PORT` is set, the grpc-gateway REST proxy is automatically started
on `GRPC_PORT + 1`.

## Proto Definitions

Protobuf definitions live in `proto/stellabill/v1/`:

- `plans.proto` — `PlanService` with `ListPlans` and `GetPlan`
- `subscriptions.proto` — `SubscriptionService` with `ListSubscriptions` and `GetSubscription`

Both services include `google.api.http` annotations for grpc-gateway REST generation.

### Regenerating Stubs

```bash
# Install tools (one-time)
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway@latest
go install github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2@latest

# Update buf dependencies
buf mod update

# Generate Go stubs, grpc-gateway handlers, and OpenAPI specs
buf generate
```

Generated files land in `gen/`:

| File | Description |
|------|-------------|
| `gen/stellabill/v1/plans.pb.go` | Plan messages (Go structs) |
| `gen/stellabill/v1/plans_grpc.pb.go` | gRPC client + server stubs |
| `gen/stellabill/v1/plans.pb.gw.go` | grpc-gateway REST handlers |
| `gen/stellabill/v1/subscriptions.pb.go` | Subscription messages |
| `gen/stellabill/v1/subscriptions_grpc.pb.go` | gRPC client + server stubs |
| `gen/stellabill/v1/subscriptions.pb.gw.go` | grpc-gateway REST handlers |
| `gen/openapiv2/stellabill.swagger.json` | Merged OpenAPI spec |

## Authentication

The gRPC server reuses the existing authentication middleware via a gRPC unary
interceptor. Two verifier paths are supported:

1. **JWT** — Standard HMAC-based JWT tokens (via `TokenGenerator`/`TokenVerifier`)
2. **SPIFFE** — Workload identity via the SPIFFE Workload API (for mesh deployments)

Tokens are passed in gRPC metadata as `authorization: Bearer <token>`.

### Example gRPC Call

```go
// Client-side
conn, _ := grpc.NewClient("localhost:50051",
    grpc.WithTransportCredentials(insecure.NewCredentials()))
defer conn.Close()

client := pb.NewPlanServiceClient(conn)
ctx := metadata.AppendToOutgoingContext(context.Background(),
    "authorization", "Bearer <jwt-token>")

resp, _ := client.ListPlans(ctx, &pb.ListPlansRequest{Limit: 10})
for _, plan := range resp.Plans {
    fmt.Printf("Plan: %s - %s\n", plan.Id, plan.Name)
}
```

### Example REST Call (via grpc-gateway)

```bash
curl -H "Authorization: Bearer <jwt-token>" \
     http://localhost:50052/api/v1/plans

curl -H "Authorization: Bearer <jwt-token>" \
     http://localhost:50052/api/v1/plans/plan-1

curl -H "Authorization: Bearer <jwt-token>" \
     http://localhost:50052/api/v1/subscriptions

curl -H "Authorization: Bearer <jwt-token>" \
     http://localhost:50052/api/v1/subscriptions/sub-1
```

## Testing

```bash
# Run gRPC-specific unit tests
go test -v -count=1 ./internal/grpc/...

# Run all tests
go test -count=1 -short ./...
```

The test suite covers:

- PlanService: ListPlans (empty, paginated, default limits, error handling)
- PlanService: GetPlan (found, not found, empty ID)
- SubscriptionService: ListSubscriptions (empty, paginated, next_billing)
- SubscriptionService: GetSubscription (found, not found, empty ID)
- Auth interceptor: successful auth, missing metadata, invalid token,
  empty bearer, no bearer prefix
- gRPC server: health check, service registration, TLS error handling
- Unauthenticated gRPC calls are properly rejected

## mTLS

To enable mutual TLS:

```bash
export GRPC_ENABLE_TLS=true
export GRPC_CERT_FILE=/path/to/server.crt
export GRPC_KEY_FILE=/path/to/server.key
export GRPC_CA_CERT_FILE=/path/to/ca.crt  # for client cert verification
```

When mTLS is enabled, clients must present a certificate signed by the
configured CA.

## Security

- Auth is enforced via a gRPC unary interceptor before any handler runs
- The interceptor reuses the same `TokenVerifier` interface as REST middleware
- Unauthenticated requests receive `gRPC status UNAUTHENTICATED (16)`
- TLS 1.2 minimum enforced when TLS is enabled
- No new attack surface on the public REST port
- gRPC port should be firewalled to internal networks only
