# Tech Stack & API Overview

This repository implements the Stellabill backend service.

## Core stack

- Language: Go 1.22+
- Framework: Gin
- Database: PostgreSQL (planned persistence) with the Outbox Pattern for reliable event publishing
- Configuration: Environment variables (no config files required for default development)

## What this backend provides (for frontends/clients)

- Health check: `GET /api/health` — verifies API and outbox dispatcher health
- Plans: `GET /api/plans` — list billing plans (id, name, amount, currency, interval, description). Currently returns an empty list; DB integration is planned.
- Subscriptions: `GET /api/subscriptions` and `GET /api/subscriptions/:id` — list and fetch subscriptions. Responses include `plan_id`, `customer`, `status`, `amount`, `interval`, `next_billing`. Currently placeholder/mock data; DB integration is planned.

CORS: Allow-all origins in development for local frontend access. Replace with explicit origins in production.

## Background worker

Production-ready background worker with features:

- Job scheduling for billing operations (charges, invoices, reminders)
- Distributed locking to prevent duplicate processing
- Retry policy with exponential backoff (example: 1s, 4s, 9s)
- Dead-letter queue for jobs that exceeded max attempts
- Graceful shutdown for in-flight work
- Metrics tracking (processed, succeeded, failed, dead-lettered)
- Concurrent worker safety

See `internal/worker/README.md` and `WORKER_IMPLEMENTATION.md` for full docs and integration guidance.

## Local setup (quick)

1. Requirements: Go 1.22+, Git. PostgreSQL optional for now.
2. Clone:

```bash
git clone https://github.com/YOUR_ORG/stellabill-backend.git
cd stellabill-backend
```

3. Dependencies:

```bash
go mod download
```

4. Environment variables (create `.env` in project root; do not commit):

Example values (development):

```
ENV=development
PORT=8080
DATABASE_URL=postgres://localhost/stellarbill?sslmode=disable
JWT_SECRET=ChangeMeNow123!Secure
ADMIN_TOKEN=AnotherStrongToken123!
```

5. Run server:

```bash
go run ./cmd/server
```

6. Verify endpoints:

```bash
curl http://localhost:8080/api/health
curl http://localhost:8080/api/plans
curl http://localhost:8080/api/subscriptions
```

## Feature flags

Flag configuration is environment-driven. Examples:

- Individual flags via `FF_` prefix (recommended), e.g. `FF_SUBSCRIPTIONS_ENABLED=true`
- JSON body via `FEATURE_FLAGS` environment variable

Priority: `FF_*` env vars > `FEATURE_FLAGS` JSON > defaults.

## Testing

- Unit tests (in-memory mocks): `go test ./internal/... -count=1 -timeout 60s`
- Integration tests (requires Docker): `go test -tags integration -v -race -count=1 -timeout 120s ./integration/...`

## Docker

- Build: `docker build -t stellabill-backend .`
- Run: `docker run --rm -e DATABASE_URL=... -p 8080:8080 stellabill-backend`

The Docker image uses a multi-stage build and runs as a nonroot user.

## CI / Quality gates

- `go build`, `go vet`, unit + integration tests, coverage enforced for `internal/` (>=95%). See `.github/workflows/ci.yml`.

## Security notes (high level)

- Never commit `.env` or secrets. Use a secrets manager in production.
- Feature flags default to fail-safe (unknown flags disabled).
- Audit logs are HMAC chained and redact sensitive fields.

## Where to find more documentation in this repo

- Worker docs: `internal/worker/README.md`
- Outbox pattern: `docs/outbox-pattern.md`
- Config and validation: `internal/config` and `internal/config/config_test.go`
- OpenAPI: `openapi/openapi.yaml` and `cmd/openapi-validate`

---

For a quick developer-oriented reference, see the linked files above and the project's main `README.md`.
