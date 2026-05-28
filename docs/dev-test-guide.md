# Local Development & Test Execution Guide

Single reference for getting the project running locally, executing the full
test suite, and resolving the most common failures. Supersedes the scattered
guidance in `QUICK_START.md`, `TEST_EXECUTION.md`, and
`README_REPOSITORY_TESTS.md`.

---

## Prerequisites

| Tool | Minimum version | Check |
|------|----------------|-------|
| Go | 1.25 | `go version` |
| Git | any | `git --version` |
| Docker | 20+ (for integration tests) | `docker info` |
| PostgreSQL | optional (Docker handles it) | — |

Install Go from [go.dev/dl](https://go.dev/dl/). Docker is only required when
running the integration test suite; unit tests have no external dependencies.

---

## 1. Clone and install dependencies

```bash
git clone https://github.com/YOUR_ORG/stellabill-backend.git
cd stellabill-backend
go mod download
```

---

## 2. Environment variables

Create a `.env` file in the project root — **never commit it**; it is already
in `.gitignore`.

```bash
# .env — local development only, do not commit
ENV=development
PORT=8080

# Required for the server to start (use placeholder values locally)
DATABASE_URL=postgres://postgres:postgres@localhost:5432/stellarbill?sslmode=disable
JWT_SECRET=Dev-Only-Secret-Change-In-Prod-1!

# Optional
ADMIN_TOKEN=dev-admin-token
AUDIT_HMAC_SECRET=stellarbill-dev-audit
AUDIT_LOG_PATH=audit.log

# Request ID trusted proxies (comma-separated CIDRs; empty = untrusted mode)
REQUEST_ID_TRUSTED_PROXIES=

# Rate limiting (disabled by default in dev)
RATE_LIMIT_ENABLED=false
```

Export them before running the server:

```bash
export $(grep -v '^#' .env | xargs)
```

> **Security:** Use your cloud provider's secrets manager in production. Never
> put real credentials in `.env` or any file tracked by Git.

---

## 3. Run the server

```bash
go run ./cmd/server
```

Verify it is up:

```bash
curl http://localhost:8080/api/health
# {"service":"stellarbill-backend","status":"ok",...}
```

---

## 4. Run the tests

### 4.1 Unit tests (no external services required)

```bash
go test ./internal/... -count=1 -timeout 60s
```

Expected: all packages pass. Coverage is enforced at ≥ 95% on `internal/`.

Generate a coverage report:

```bash
go test ./internal/... \
  -covermode=atomic \
  -coverpkg=./internal/... \
  -coverprofile=coverage.out \
  -count=1 \
  -timeout 60s

go tool cover -html=coverage.out   # opens browser
```

### 4.2 Integration tests (Docker required)

Integration tests spin up an ephemeral Postgres container automatically via
`testcontainers-go`. No manual database setup is needed.

```bash
go test -tags integration -v -race -count=1 -timeout 120s ./integration/...
```

`TestMain` applies all SQL migrations before any test case runs. The container
is torn down automatically when the suite finishes.

### 4.3 Race detector

```bash
go test ./internal/... -race -count=1 -timeout 60s
```

### 4.4 Specific packages

```bash
# Middleware
go test ./internal/middleware/... -v -count=1

# Worker
go test ./internal/worker/... -v -count=1

# Outbox
go test ./internal/outbox/... -v -count=1

# Config
go test ./internal/config/... -v -count=1

# Audit
go test ./internal/audit/... -v -count=1
```

### 4.5 Pre-PR checklist

```bash
go build ./...          # must compile cleanly
go vet ./...            # no vet warnings
go fmt ./...            # code is formatted
go test ./internal/... -count=1 -timeout 60s   # all tests pass
```

---

## 5. Database migrations

```bash
go run ./cmd/migrate up
```

Migrations live in `migrations/`. See `docs/migrations.md` for conventions.

---

## 6. Troubleshooting

### 6.1 Server fails to start — missing environment variables

**Symptom:**
```
config error [MISSING_ENV_VAR]: required secret is missing (key=DATABASE_URL)
```

**Fix:** Export the required variables before running:
```bash
export DATABASE_URL=postgres://postgres:postgres@localhost:5432/stellarbill?sslmode=disable
export JWT_SECRET=Dev-Only-Secret-Change-In-Prod-1!
go run ./cmd/server
```

---

### 6.2 Server fails to start — weak JWT_SECRET

**Symptom:**
```
config error [WEAK_SECRET]: must be at least 12 characters and contain mixed
alphanumeric and special characters (key=JWT_SECRET)
```

**Fix:** Use a secret with uppercase, lowercase, digits, and a special
character, minimum 12 characters:
```bash
export JWT_SECRET=Dev-Only-Secret-Change-In-Prod-1!
```

---

### 6.3 Database connection refused

**Symptom:**
```
dial tcp 127.0.0.1:5432: connect: connection refused
```

**Fix options:**

a) Start Postgres locally:
```bash
docker run -d \
  --name stellarbill-pg \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=stellarbill \
  -p 5432:5432 \
  postgres:16-alpine
```

b) Or point `DATABASE_URL` at an existing instance.

c) The server runs without a real DB for most endpoints (mock repositories are
used by default in dev). Only endpoints that require persistence will fail.

---

### 6.4 Migration fails — table already exists

**Symptom:**
```
pq: relation "plans" already exists
```

**Fix:** Run the down migration first, then up:
```bash
go run ./cmd/migrate down
go run ./cmd/migrate up
```

Or drop and recreate the database:
```bash
docker exec -it stellarbill-pg psql -U postgres -c "DROP DATABASE stellarbill;"
docker exec -it stellarbill-pg psql -U postgres -c "CREATE DATABASE stellarbill;"
go run ./cmd/migrate up
```

---

### 6.5 Integration tests fail — Docker not running

**Symptom:**
```
Cannot connect to the Docker daemon at unix:///var/run/docker.sock
```

**Fix:** Start Docker Desktop (macOS/Windows) or the Docker daemon (Linux):
```bash
sudo systemctl start docker   # Linux
```

Then re-run:
```bash
go test -tags integration -v -count=1 -timeout 120s ./integration/...
```

---

### 6.6 Integration tests fail — container startup timeout

**Symptom:**
```
context deadline exceeded waiting for container to be ready
```

**Fix:** Increase the timeout or check Docker resource limits:
```bash
go test -tags integration -v -count=1 -timeout 300s ./integration/...
```

If Docker is resource-constrained, increase memory allocation in Docker
Desktop settings (≥ 4 GB recommended).

---

### 6.7 Auth failures in tests — 401 Unauthorized

**Symptom:** Tests that hit protected endpoints return 401 unexpectedly.

**Cause:** The `Authorization` header is missing or the bearer token does not
match `JWT_SECRET`.

**Fix:** Ensure the test sets the correct header:
```go
req.Header.Set("Authorization", "Bearer "+jwtSecret)
```

For integration tests, `JWT_SECRET` is set in `TestMain` via
`os.Setenv("JWT_SECRET", ...)`. Check `integration/main_test.go` if the value
has drifted.

---

### 6.8 Request ID not propagated — missing X-Request-ID header

**Symptom:** Responses lack `X-Request-ID` or logs show a different ID than
the one sent.

**Cause:** The inbound `X-Request-ID` is only accepted from trusted sources.
If `REQUEST_ID_TRUSTED_PROXIES` is empty (the default), all inbound IDs are
discarded and a new one is generated.

**Fix for local testing with curl:**
```bash
# Without trusted proxies — server generates its own ID
curl -H "X-Request-ID: my-trace-id" http://localhost:8080/api/health
# Response X-Request-ID will be a generated ID, not "my-trace-id"

# To accept inbound IDs, add your IP to the allowlist
export REQUEST_ID_TRUSTED_PROXIES=127.0.0.1/32
go run ./cmd/server
curl -H "X-Request-ID: my-trace-id" http://localhost:8080/api/health
# Response X-Request-ID: my-trace-id
```

---

### 6.9 Tests hang — goroutine leak in rate limiter

**Symptom:** `go test` hangs and eventually times out with a goroutine dump
showing `cleanupExpiredBuckets` blocked on a ticker.

**Cause:** `APIRateLimiter` starts a background goroutine. Tests that create
one must call `rl.Stop()` to release it.

**Fix:** Always defer `Stop()` in tests:
```go
rl := middleware.NewAPIRateLimiter(config)
defer rl.Stop()
```

---

### 6.10 Build fails — duplicate constant declarations

**Symptom:**
```
./requestid.go:8:2: RequestIDHeader redeclared in this block
```

**Cause:** `internal/middleware/requestid.go` (now deleted) conflicted with
`internal/middleware/middleware.go`. If you see this after a merge or rebase,
ensure `internal/middleware/requestid.go` does not exist:
```bash
ls internal/middleware/requestid.go   # should not exist
```

If it does, delete it — all request ID logic now lives in
`internal/requestid/requestid.go`.

---

### 6.11 `go test ./...` fails on `cmd/server`

**Symptom:**
```
FAIL stellarbill-backend/cmd/server [setup failed]
```

**Cause:** `cmd/server/main.go` is the process entry point and cannot be
instrumented as a unit-testable package. This is expected.

**Fix:** Run tests on `internal/` only:
```bash
go test ./internal/... -count=1 -timeout 60s
```

---

### 6.12 Coverage below 95%

**Symptom:**
```
coverage: 87.3% of statements
```

**Fix:** Find uncovered lines:
```bash
go test ./internal/... \
  -coverprofile=coverage.out \
  -coverpkg=./internal/... \
  -count=1

go tool cover -func=coverage.out | sort -t% -k1 -n | head -20
```

Add tests for the uncovered paths, focusing on error branches and edge cases.

---

## 7. CI reference

The GitHub Actions workflow (`.github/workflows/ci.yml`) runs on every push
and pull request:

| Step | Command |
|------|---------|
| Build | `go build ./...` |
| Vet | `go vet ./...` |
| Unit tests + coverage | `go test ./internal/... -covermode=atomic -coverpkg=./internal/...` |
| Coverage threshold | ≥ 95% on `internal/` |

Coverage artifacts are retained for 14 days.

---

## 8. Security notes

- Never commit `.env`, JWT secrets, database credentials, or API keys.
- `REQUEST_ID_TRUSTED_PROXIES` defaults to empty (untrusted mode). Only add
  CIDRs you control (e.g., your load balancer's egress range).
- The in-memory rate limiter is process-local. In multi-instance deployments,
  use a shared store (Redis) or rely on upstream rate limiting.
- CORS is configured as `*` in development. Set an explicit origin in
  production via the `CORS_ALLOW_ORIGIN` environment variable.
- Audit log entries are HMAC-signed. Protect `AUDIT_HMAC_SECRET` as you would
  any signing key.

---

## 9. Related documents

| Document | Purpose |
|----------|---------|
| `README.md` | Project overview, API reference, contributing guide |
| `docs/migrations.md` | Migration conventions and production runbook |
| `docs/RATE_LIMITING.md` | Rate limiting configuration and security notes |
| `docs/outbox-pattern.md` | Outbox pattern design and operational notes |
| `docs/runbooks/outbox-operations.md` | Outbox operational runbook |
| `docs/security-notes.md` | Security analysis and threat model |
| `internal/worker/README.md` | Background worker documentation |
