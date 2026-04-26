# Stellabill Backend

Go (Gin) API backend for Stellabill - subscription and billing plans API. This repo is backend-only; a separate frontend consumes these APIs.

---

## Table of contents

- [Tech stack](#tech-stack)
- [What this backend provides (for the frontend)](#what-this-backend-provides-for-the-frontend)
- [Background Worker](#background-worker)
- [Local setup](#local-setup)
- [Configuration](#configuration)
- [Testing](#testing)
- [API reference](#api-reference)
- [Database migrations](#database-migrations)
- [Contributing (open source)](#contributing-open-source)
- [Project layout](#project-layout)
- [License](#license)

---

## Tech stack

- **Language:** Go 1.22+
- **Framework:** [Gin](https://github.com/gin-gonic/gin)
- **Database:** PostgreSQL with [Outbox Pattern](https://microservices.io/patterns/data/transactional-outbox.html) for reliable event publishing
- **Config:** Environment variables (no config files required for default dev)

---

## What this backend provides (for the frontend)

This service is the **backend only**. A separate frontend (or any client) can:

- **Health check** - `GET /api/health` to verify the API is up.
- **Plans** - `GET /api/plans` to list billing plans (id, name, amount, currency, interval, description). Currently returns an empty list; DB integration is planned.
- **Subscriptions** - `GET /api/subscriptions` to list subscriptions and `GET /api/subscriptions/:id` to fetch one. Responses include plan_id, customer, status, amount, interval, next_billing. Currently placeholder/mock data; DB integration is planned.

CORS is enabled for all origins in development so a frontend on another port or domain can call these endpoints.

---

## Background Worker

The backend includes a production-ready background worker system for automated billing job scheduling and execution.

### Key Features

- **Job Scheduling**: Schedule billing operations (charges, invoices, reminders) with configurable execution times
- **Distributed Locking**: Prevents duplicate processing when running multiple worker instances
- **Retry Policy**: Automatic retry with exponential backoff (1s, 4s, 9s) for failed jobs
- **Dead-Letter Queue**: Failed jobs after max attempts are moved for manual review
- **Graceful Shutdown**: Workers complete in-flight jobs before shutting down
- **Metrics Tracking**: Monitor job processing statistics (processed, succeeded, failed, dead-lettered)
- **Concurrent Workers**: Multiple workers can run safely without duplicate processing

### Documentation

- `internal/worker/README.md` - Complete worker documentation
- `internal/worker/INTEGRATION.md` - Integration guide with examples
- `internal/worker/SECURITY.md` - Security analysis and threat model
- `WORKER_IMPLEMENTATION.md` - Implementation summary

### Quick Example

```go
store := worker.NewMemoryStore()
executor := worker.NewBillingExecutor()
config := worker.DefaultConfig()

w := worker.NewWorker(store, executor, config)
w.Start()
defer w.Stop()

scheduler := worker.NewScheduler(store)
job, _ := scheduler.ScheduleCharge("sub-123", time.Now(), 3)
```

---

## Local setup

### Prerequisites

- **Go 1.22 or later**
  - Check: `go version`
  - Install: [https://go.dev/doc/install](https://go.dev/doc/install)
- **Git** (for cloning and contributing)
- **PostgreSQL** (optional for now; app runs without it using default config; DB will be used when persistence is added)

### 1. Clone the repository

```bash
git clone https://github.com/YOUR_ORG/stellabill-backend.git
cd stellabill-backend
```

### 2. Install dependencies

```bash
go mod download
```

### 3. Environment variables (required for secure startup)

Create a `.env` file in the project root (do not commit it; it is in `.gitignore`):

```bash
# Required for startup
ENV=development
PORT=8080
DATABASE_URL=postgres://localhost/stellarbill?sslmode=disable
JWT_SECRET=ChangeMeNow123!Secure
ADMIN_TOKEN=AnotherStrongToken123!

# Required in production/staging (comma-separated https origins)
ALLOWED_ORIGINS=https://app.example.com,https://admin.example.com

# Optional with validation
RATE_LIMIT_ENABLED=true
RATE_LIMIT_MODE=ip
RATE_LIMIT_RPS=10
RATE_LIMIT_BURST=20
RATE_LIMIT_WHITELIST=/api/health
READ_TIMEOUT=30
WRITE_TIMEOUT=30
IDLE_TIMEOUT=120
MAX_HEADER_BYTES=1048576
AUDIT_HMAC_SECRET=stellarbill-dev-audit
AUDIT_LOG_PATH=audit.log
```

Or export them in your shell. The app now fails fast when required values are missing or insecure.

### 4. Run the server

```bash
go run ./cmd/server
```

Server listens on `http://localhost:8080` (or the port you set via `PORT`).

### 5. Verify

```bash
curl http://localhost:8080/api/health
# Expected: {"service":"stellarbill-backend","status":"ok","outbox":{"pending_events":0,"dispatcher_running":true,"database_health":"healthy"}}

curl http://localhost:8080/api/outbox/stats
# Expected: {"pending_events":0,"dispatcher_running":true,"database_health":"healthy"}

curl -X POST http://localhost:8080/api/outbox/test
# Expected: {"message":"Test event published successfully","event_type":"test.event"}
```

---

## Configuration

| Variable        | Default                                      | Description                    |
|----------------|----------------------------------------------|--------------------------------|
| `ENV`          | `development`                                | Environment (e.g. production)  |
| `PORT`         | `8080`                                       | HTTP server port               |
| `DATABASE_URL` | None (required) | PostgreSQL connection string (must be valid URL) |
| `JWT_SECRET`   | None (required) | Secret for JWT (minimum 12 chars with upper/lower/digit/special) |
| `ADMIN_TOKEN`  | None (required) | Admin endpoint token (minimum 12 chars with upper/lower/digit/special) |
| `ALLOWED_ORIGINS` | Required in `production`/`staging` | Comma-separated `https://` origins for CORS |
| `RATE_LIMIT_MODE` | `ip` | One of: `ip`, `user`, `hybrid` |
| `RATE_LIMIT_RPS` | `10` | Integer between `1` and `1000` |
| `RATE_LIMIT_BURST` | `20` | Integer between `1` and `5000`, must be `>= RATE_LIMIT_RPS` |
| `READ_TIMEOUT` | `30` | Timeout in seconds, range `1` to `3600` |
| `WRITE_TIMEOUT` | `30` | Timeout in seconds, range `1` to `3600` |
| `IDLE_TIMEOUT` | `120` | Timeout in seconds, range `1` to `3600` |
| `MAX_HEADER_BYTES` | `1048576` | Header size in bytes, range `1024` to `16777216` |
| `FF_DEFAULT_ENABLED` | `false`                                | Default state for unknown flags |
| `FF_LOG_DISABLED` | `true`                                  | Log when flags block requests  |
| `FF_CONFIG_FILE` | `""`                                    | Path to feature flags config file |

### Feature Flags Configuration

Feature flags can be configured using environment variables in several ways:

#### 1. Individual Flags (Recommended)
Use the `FF_` prefix for individual flags:
```bash
# Enable/disable specific features
FF_SUBSCRIPTIONS_ENABLED=true
FF_PLANS_ENABLED=false
FF_NEW_BILLING_FLOW=true
FF_ADVANCED_ANALYTICS=false
```

#### 2. JSON Configuration
Use the `FEATURE_FLAGS` environment variable for bulk configuration:
```bash
export FEATURE_FLAGS='{"subscriptions_enabled": true, "plans_enabled": true, "new_billing_flow": false}'
```

#### 3. Priority Order
The system uses the following priority (highest to lowest):
1. `FF_*` individual environment variables
2. `FEATURE_FLAGS` JSON configuration
3. Default flag values

#### Available Feature Flags

| Flag Name | Default | Description |
|-----------|---------|-------------|
| `subscriptions_enabled` | `true` | Enable subscription management endpoints |
| `plans_enabled` | `true` | Enable billing plans endpoints |
| `new_billing_flow` | `false` | Enable new billing flow feature |
| `advanced_analytics` | `false` | Enable advanced analytics endpoints |

In production, set these via your host's environment or secrets manager; do not commit secrets.

---

## Using Feature Flags in Code

```go
import "stellarbill-backend/internal/middleware"
import "stellarbill-backend/internal/featureflags"

// Method 1: Middleware (recommended for endpoints)
router.GET("/feature", middleware.FeatureFlag("my_feature"), handler)

// Method 2: With default value
router.GET("/feature", middleware.FeatureFlagWithDefault("my_feature", true), handler)

// Method 3: Direct check in code
if featureflags.IsEnabled("my_feature") {
    // Feature code here
}

// Method 4: Multiple flags requirement
router.GET("/feature", middleware.RequireAllFeatureFlags("flag1", "flag2"), handler)
router.GET("/feature", middleware.RequireAnyFeatureFlags("flag1", "flag2"), handler)
```

---

## Testing

> See **[docs/dev-test-guide.md](docs/dev-test-guide.md)** for the full local
> development and test execution guide, including common failure
> troubleshooting.

### Unit tests

Unit tests cover config validation, service logic, HTTP handler behaviour,
circuit breaker, and the background worker. They use in-memory mocks and
require **no external services**.

```bash
go test ./internal/... -count=1 -timeout 60s
```

### Integration tests

Integration tests spin up a real ephemeral Postgres container via Docker and
validate the full request path — from route handler through service and
repository to the database — then tear the container down automatically.

**Prerequisites:** Docker must be running locally (or in CI with Docker socket
access). No manual database setup is required.

```bash
go test -tags integration -v -race -count=1 -timeout 120s ./integration/...
```

The test suite in `integration/` covers:

| Scenario | Expected |
|---|---|
| Owner fetches own active subscription | 200 with full plan + billing envelope |
| Unknown subscription ID | 404 |
| Soft-deleted subscription | 410 |
| Caller does not own the subscription | 403 |
| Missing `Authorization` header | 401 |
| Malformed JWT | 401 |
| Subscription exists but referenced plan is missing | 200 with `"plan not found"` warning |
| Subscription has non-numeric amount | 500 |
| 10 concurrent reads of the same subscription | all 200, no data race |
| `GET /api/health` | 200 |
| `GET /api/plans` | 200 |
| `GET /api/subscriptions` | 200 |

**Migration timing and startup race handling:** `TestMain` applies all SQL
migrations before any test runs. The Postgres container wait strategy requires
the ready-to-accept-connections log line to appear **twice** (once during
recovery init, once when actually ready), preventing false-positive startup
races.

**CI example:**

```yaml
- name: Integration tests
  run: go test -tags integration -race -count=1 -timeout 120s ./integration/...
```

---

## API reference

Base URL (local): `http://localhost:8080`

| Method | Path                     | Feature Flag Required | Description              |
|--------|--------------------------|---------------------|--------------------------|
| GET    | `/api/health`            | None                | Health check             |
| GET    | `/api/plans`             | `plans_enabled` (default: true) | List billing plans       |
| GET    | `/api/subscriptions`     | `subscriptions_enabled` (default: true) | List subscriptions       |
| GET    | `/api/subscriptions/:id` | `subscriptions_enabled` (default: true) | Get one subscription     |
| GET    | `/api/billing/new-flow`  | `new_billing_flow` (default: false) | New billing flow feature |
| GET    | `/api/analytics/advanced` | `advanced_analytics` AND `subscriptions_enabled` | Advanced analytics |

All JSON responses. CORS allowed for `*` origin with common methods and headers.

**Feature Flag Responses**: When a feature flag blocks a request, the API returns:
```json
{
  "error": "feature_unavailable",
  "message": "This feature is currently unavailable",
  "feature_flag": "flag_name"
}
```

---

## Database migrations

Migrations live in `migrations/` and are applied with:

```bash
go run ./cmd/migrate up
```

See `docs/migrations.md` for conventions and a production runbook.

---

## CI / Quality gates

Every push and pull request runs the following checks automatically via GitHub Actions (`.github/workflows/ci.yml`):

| Step | Command |
|------|---------|
| Build | `go build ./...` |
| Vet | `go vet ./...` |
| Test + coverage | `go test ./internal/... -covermode=atomic -coverpkg=./internal/...` |
| Coverage threshold | `./scripts/check-coverage.sh coverage.out 95` (≥ 95 % on `internal/`) |

Coverage artifacts (`coverage.out`) are uploaded and retained for 14 days on every run.

### Run checks locally before opening a PR

```bash
# 1. Build
go build ./...

# 2. Vet
go vet ./...

# 3. Test with coverage (internal packages only — cmd/server is the process entrypoint)
go test ./internal/... \
  -covermode=atomic \
  -coverpkg=./internal/... \
  -coverprofile=coverage.out \
  -count=1 \
  -timeout=60s

# 4. Enforce the 95 % threshold
./scripts/check-coverage.sh coverage.out 95

# 5. (Optional) Browse the HTML report
go tool cover -html=coverage.out
```

> **Why `./internal/...` and not `./...`?**  
> `cmd/server/main.go` is the process entry point (`main()`). Go cannot instrument it as a unit-testable package, so it always reports 0 % and would drag the total below the threshold. All business logic lives in `internal/`, which is what the threshold enforces.

> **Security note:** Never commit `.env`, JWT secrets, or database credentials. The CI workflow contains no secrets; configure them via your host's environment or a secrets manager.

---

## Middleware order

Recommended order for the HTTP chain:

1. `recovery`
2. `request-id`
3. `logging`
4. `cors`
5. `rate-limit`
6. `auth` for protected routes only

Why this order:

- `recovery` wraps the full chain so panics from downstream middleware and handlers are converted into structured `500` responses.
- `request-id` runs early so every response and log line can carry the same correlation ID.
- `logging` runs before short-circuiting middleware so failed auth, rate-limit, and panic-recovery responses are still logged.
- `cors` handles preflight `OPTIONS` requests before rate limiting or auth rejects them.
- `rate-limit` runs before `auth` on protected routes to reduce brute-force pressure on authentication logic.
- `auth` should be attached only to protected groups so public endpoints like `/api/health` can remain reachable.

Behavior verified by tests:

- Middleware entry and unwind order.
- Request ID propagation across middleware and handlers.
- Expected short-circuit responses for preflight, auth failures, rate limiting, and panic recovery.

Security notes:

- `X-Request-ID` input is sanitized before reuse in logs and responses.
- The in-memory rate limiter is process-local and keyed by client IP, so deployments behind proxies should ensure trusted forwarding headers are configured correctly.
- CORS is currently configured as `*`; production deployments should replace that with an explicit frontend origin.
- The sample auth middleware validates a bearer token against the configured secret and is intended as a lightweight guard for protected groups until full JWT validation is introduced.

---

## Dependency Injection

Handlers are constructed with explicit dependencies instead of reaching into package-level state. That keeps startup wiring easy to review and makes unit tests cheap to write because services can be replaced with focused mocks.

Current boundaries:

- `internal/services` defines the interfaces and default placeholder implementations used by the API.
- `internal/handlers` validates constructor input and translates service results into HTTP responses.
- `internal/routes` requires an injected handler bundle and returns an error on nil wiring instead of registering a partially working router.
- `cmd/server` is responsible for composing concrete services and failing fast if startup wiring is incomplete.

Security notes:

- Constructor validation prevents nil dependencies from reaching request handling paths, which avoids panic-driven denial of service during misconfigured startup.
- Route registration returns errors for missing router or handler wiring so invalid startup state fails closed.
- Service interfaces keep handlers decoupled from future storage implementations, making authorization and data-access checks easier to test in isolation.

---

## Audit logging

- **Tamper-evident chain:** Each audit entry is HMAC-signed with `AUDIT_HMAC_SECRET` and linked to the previous hash (chain-of-trust). Breaking or removing a line invalidates later hashes.
- **What gets logged:** `actor`, `action`, `target`, `outcome`, request method/path, client IP, and any supplied metadata (e.g., attempts, reasons).
- **Redaction:** Sensitive fields such as tokens, passwords, secrets, Authorization headers, and values that *look* like bearer/basic credentials are stored as `[REDACTED]`.
- **Sink:** Default sink writes JSON Lines to `AUDIT_LOG_PATH` (default `audit.log`). File permissions are `0600` on creation.
- **Admin example:** `POST /api/admin/purge` demonstrates a sensitive operation. Success, partial success (`?partial=1`), denied access, and retry attempts are all audit-logged.
- **Auth failures:** 401/403 responses are automatically logged via middleware, with headers redacted.

---

## Testing

```
go test ./... -cover
```

Tests include redaction coverage, hash chaining, admin action logging, and middleware auth-failure logging. Coverage currently exceeds 95%.

---

## Contributing (open source)

We welcome contributions from the community. Below is a short guide to get you from "first look" to "merged change".

### Code of conduct

- Be respectful and inclusive.
- Focus on constructive feedback and clear, factual communication.

### How to contribute

1. **Open an issue**
   - Bug: describe what you did, what you expected, and what happened.
   - Feature: describe the goal and why it helps.
2. **Fork and clone**
   - Fork the repo on GitHub, then clone your fork locally.
3. **Create a branch**
   ```bash
   git checkout -b fix/your-fix   # or feature/your-feature
   ```
4. **Make changes**
   - Follow existing style (format with `go fmt`).
   - Keep commits logical and messages clear (e.g. "Add validation for plan ID").
5. **Run checks**
   ```bash
   go build ./...
   go vet ./...
   go fmt ./...
   ```
   Add or run tests if the project has them.
6. **Commit**
   - Prefer small, atomic commits (one logical change per commit).
7. **Push and open a PR**
   ```bash
   git push origin fix/your-fix
   ```
   - Open a Pull Request against the main branch.
   - Fill in the PR template (if any).
   - Link related issues.
   - Describe what you changed and why.
8. **Review**
   - Address review comments. Maintainers will merge when everything looks good.

### Development workflow

- Use the [Local setup](#local-setup) steps to run the server.
- Change code, restart the server (or use a tool like `air` for live reload if the project adds it).
- Test with `curl` or the frontend that consumes this API.

### Project standards

- **Go:** `go fmt`, `go vet`, no unnecessary dependencies.
- **APIs:** Keep JSON shape stable; document breaking changes in PRs.
- **Secrets:** Never commit `.env`, keys, or passwords.

---

## Project layout

```text
stellabill-backend/
├── .github/
│   └── workflows/
│       └── ci.yml           # CI: build, vet, test, coverage threshold
├── cmd/
│   └── server/
│       └── main.go          # Entry point, Gin router, server start
├── docs/
│   ├── outbox-pattern.md    # Outbox pattern documentation
│   └── security-notes.md    # Security considerations
├── internal/
│   ├── config/
│   │   └── config.go        # Loads ENV, PORT, DATABASE_URL, JWT_SECRET, feature flags
│   ├── featureflags/
│   │   ├── featureflags.go   # Feature flag management system
│   │   └── featureflags_test.go # Unit tests for feature flags
│   ├── middleware/
│   │   ├── featureflags.go   # Feature flag middleware for endpoint gating
│   │   └── featureflags_test.go # Middleware tests
│   ├── handlers/
│   │   ├── health.go        # GET /api/health (includes outbox status)
│   │   ├── plans.go         # GET /api/plans
│   │   └── subscriptions.go # GET /api/subscriptions, /api/subscriptions/:id
│   ├── routes/
│   │   └── routes.go                # Registers routes and CORS middleware
│   ├── service/
│   │   └── subscription_service.go  # Business logic — ownership, soft-delete, billing
│   ├── testutil/
│   │   └── db.go                    # Ephemeral container lifecycle helpers
│   └── worker/
│       ├── job.go                   # Job model and JobStore interface
│       ├── store_memory.go          # In-memory JobStore implementation
│       ├── worker.go                # Background worker with scheduler loop
│       ├── executor.go              # Billing job executor
│       └── scheduler.go             # Job scheduling utilities
├── migrations/
│   ├── migrations.go                # embed.FS export for the SQL files
│   ├── 001_create_plans.sql
│   └── 002_create_subscriptions.sql
├── go.mod
├── go.sum
└── README.md
```

---

## Security Considerations

### Feature Flags Security

- **Environment Variables**: Feature flags are configured via environment variables, which are secure and not committed to version control
- **Default Behavior**: Unknown flags default to `false` for security (fail-safe)
- **No Dynamic Loading**: Flags are loaded at startup only, preventing runtime injection attacks
- **Thread Safety**: All flag operations are thread-safe with proper mutex locking
- **Validation**: Invalid flag values are safely ignored and logged

### Best Practices

1. **Production Flags**: Always set explicit flag values in production; don't rely on defaults
2. **Secret Management**: Use your cloud provider's secret manager for sensitive flag configurations
3. **Monitoring**: Monitor flag usage and access patterns
4. **Audit Trail**: Flag changes are tracked with timestamps for auditing
5. **Testing**: Test both enabled and disabled states in your test suite

### Testing Security

The feature flag system includes comprehensive tests covering:
- Concurrent access and race conditions
- Invalid input handling
- Memory leak prevention
- Environment variable injection attempts
- Edge cases and error conditions

Run tests with: `go test ./...`

---

## License

See the LICENSE file in the repository (if present). If none, assume proprietary until stated otherwise.
