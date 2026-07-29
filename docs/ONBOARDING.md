# Onboarding Runbook — Stellabill Backend

> **Goal:** Go from a clean laptop to green tests in under one hour.

---

## 1. Prerequisites

| Tool         | Version (min) | macOS (Apple Silicon) | Linux (amd64) |
|--------------|---------------|-----------------------|---------------|
| **Go**       | 1.22          | `brew install go`     | `apt install golang-go` or [tarball](https://go.dev/dl/) |
| **PostgreSQL** | 15, 16, or 17 | `brew install postgresql@15` | `apt install postgresql-15` |
| **git**      | Any modern    | pre‑installed / Xcode | `apt install git` |
| **make**     | Any modern    | `brew install make`   | `apt install build-essential` |
| **golangci-lint** | 1.60    | `brew install golangci-lint` | [binary](https://golangci-lint.run/usage/install/) |
| **Docker** (optional) | latest | [Docker Desktop](https://docs.docker.com/desktop/install/mac/) | [Docker Engine](https://docs.docker.com/engine/install/ubuntu/) |

Verify installation:

```bash
go version     # → go 1.22.x
psql --version # → psql 15.x / 16.x / 17.x
```

---

## 2. Clone & toolchain

```bash
git clone https://github.com/Stellabill/stellabill-backend.git
cd stellabill-backend

# Download Go module dependencies
go mod download

# Verify the toolchain compiles
go build ./...
```

All dependencies are vendored in `go.sum`; the build should be hermetic.

---

## 3. Database setup

### 3.1 Create the database

```bash
# Connect as the postgres superuser
psql -U postgres

CREATE DATABASE stellabill_dev;
CREATE USER stellabill WITH PASSWORD 'stellabill_dev';
GRANT ALL PRIVILEGES ON DATABASE stellabill_dev TO stellabill;
\c stellabill_dev
GRANT ALL ON SCHEMA public TO stellabill;
\q
```

### 3.2 Run migrations

```bash
# Migrations live in migrations/ and are run via the application.
# The server will apply pending migrations on startup when MIGRATE_ON_START=true.
# For a one-shot migration run:
go run cmd/server/main.go --migrate-only
```

### 3.3 Seed sample data (if applicable)

Currently no seed script exists. Plans are tracked in issue #200. For development,
you can use the `POST /api/subscriptions` endpoint once integrated.

---

## 4. Configuration

Copy the example env file and adjust as needed:

```bash
cp .env.example .env
```

Key variables:

| Variable           | Default               | Description                              |
|--------------------|-----------------------|------------------------------------------|
| `PORT`             | `8080`                | HTTP listen port                         |
| `DATABASE_URL`     | *(see .env.example)*  | Postgres connection string               |
| `MIGRATE_ON_START` | `true`                | Auto-apply migrations on startup         |
| `LOG_LEVEL`        | `info`                | Logging verbosity (`debug`, `info`, etc) |
| `CORS_ORIGINS`     | `*`                   | Allowed CORS origins                     |

---

## 5. Running tests

```bash
# Run all tests (unit + integration, excluding E2E)
go test ./... -count=1 -timeout 120s

# Run only unit tests (fast, no DB required for pure unit)
go test ./internal/... -short -count=1

# Run with coverage
go test ./... -coverprofile=coverage.out -covermode=atomic

# View coverage in the browser
go tool cover -html=coverage.out
```

**Expected output:** All tests pass with **≥ 80 %** statement coverage across the whole codebase.

---

## 6. Linting & static analysis

```bash
# Run the linter (uses .golangci.yml at project root)
golangci-lint run ./...

# Run go vet (always passes in CI)
go vet ./...
```

---

## 7. Running the server locally

```bash
# Start with hot-reload (requires air or fresh):
#   go install github.com/air-verse/air@latest
#   air

# Or start directly:
go run cmd/server/main.go

# Health check:
curl http://localhost:8080/api/health
# → {"status":"ok"}
```

---

## 8. Top 5 files to read

| File | Why |
|------|-----|
| **[cmd/server/main.go](../cmd/server/main.go)** | Application entrypoint — dependency wiring, middleware stack, and server lifecycle. |
| **[internal/config/config.go](../internal/config/config.go)** | Configuration loading from environment variables — every knob is documented here. |
| **[internal/auth/jwt.go](../internal/auth/jwt.go)** | JWT verification, claims extraction, and middleware plumbing — core security boundary. |
| **[internal/db/pool.go](../internal/db/pool.go)** | Database connection pool, transaction management, and row-level security. |
| **[internal/graphql/schema.go](../internal/graphql/schema.go)** | GraphQL schema definitions — the public API surface; start here when adding a new query or mutation. |

---

## 9. Project layout

```
stellabill-backend/
├── cmd/                  # Entrypoints
│   └── server/           #   HTTP server entrypoint
├── deploy/               # Deployment manifests
├── docs/                 # Documentation
├── internal/             # Application code (not importable from outside)
│   ├── adr/              #   Architecture Decision Records
│   ├── audit/            #   Audit logging
│   ├── auth/             #   Authentication & authorization
│   ├── cache/            #   In-memory & Redis caching
│   ├── config/           #   Configuration
│   ├── correlation/      #   Request correlation IDs
│   ├── db/               #   Database pool & migrations
│   ├── featureflags/     #   Feature flag evaluation
│   ├── graphql/          #   GraphQL handler + schema
│   ├── middleware/       #   HTTP middleware
│   ├── opentelemetry/    #   OpenTelemetry tracing
│   └── ...               #   More modules as added
├── migrations/           # SQL migration files
├── .golangci.yml         # Linter configuration
├── go.mod                # Go module definition
└── README.md             # Project overview
```

---

## 10. Troubleshooting matrix

| Symptom | Likely cause | Fix |
|---------|-------------|-----|
| `go build` fails with missing module | Go proxy unreachable or module cache stale | `go env -w GOPROXY=https://proxy.golang.org,direct && go mod download` |
| `psql: connection refused` | PostgreSQL not running | `brew services start postgresql@15` (macOS) or `sudo systemctl start postgresql` (Linux) |
| `pq: role "stellabill" does not exist` | DB user not created | Run the `CREATE USER` step from [§3.1](#31-create-the-database) |
| `pq: database "stellabill_dev" does not exist` | Database not created | Run `CREATE DATABASE stellabill_dev;` as postgres superuser |
| Test fails with `dial tcp 127.0.0.1:5432: connect: connection refused` | DB not running or `DATABASE_URL` wrong | Verify PostgreSQL is running; check `DATABASE_URL` in `.env` |
| `golangci-lint` not found | Tool not installed | `brew install golangci-lint` or download from [releases](https://github.com/golangci/golangci-lint/releases) |
| `go test` times out | Integration tests need a running DB | Start PostgreSQL first, or run with `-short` to skip integration tests |
| `air: command not found` | Hot-reload tool not installed | `go install github.com/air-verse/air@latest` |
| `M1/M2` `brew` fails on `postgresql@15` | Rosetta 2 not installed on Apple Silicon | `softwareupdate --install-rosetta` or use Postgres.app |

---

## 11. CI pipeline (quick reference)

The CI runs on every push and pull request via `.github/workflows/ci.yml`:

1. **Lint** — `golangci-lint run ./... --timeout 5m`
2. **Build** — `go build ./...`
3. **Test** — `go test ./... -count=1 -timeout 120s -coverprofile=coverage.out`
4. **Coverage** — Upload to Codecov (threshold: 80 %)

---

## Appendix: macOS Apple Silicon vs Linux amd64 notes

- **Apple Silicon:** All tools work via Rosetta 2 or native ARM builds. Go 1.22+ ships native ARM binaries.
- **Linux amd64:** Standard packages from apt or tarballs. No special configuration needed.
- **PostgreSQL:** On Apple Silicon, prefer Postgres.app or `brew install postgresql@15` (native ARM). On Linux, `apt install postgresql-15` provides amd64.
- **Docker images:** The `Dockerfile` is multi-arch (`linux/amd64`, `linux/arm64`). CI builds both.
