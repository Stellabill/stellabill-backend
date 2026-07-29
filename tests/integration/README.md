# Local integration stack

These tests start Postgres, Redis, and an HTTP echo server with
`testcontainers-go`. Docker Compose and fixed host ports are not required.

## Run

Docker must be running:

```bash
go test -tags integration -race -count=1 -timeout 5m ./tests/integration/...
```

To compile the package without starting containers:

```bash
go test -c -tags integration ./tests/integration
```

The repository currently requires the `integration` build tag; running
`go test ./tests/integration/...` without it intentionally matches no package.

## Lifecycle and parallelism

`TestMain` starts one stack per test process. All tests in that process share
the stack, and every generic request sets `Reuse: true`. Container names carry
a cryptographically random run identifier so independently running test
processes do not reuse one another's state. Docker chooses each host port, and
the parallelism test starts two copies of the webhook receiver and verifies
that their endpoints differ.

## Security assumptions

- Postgres and Redis credentials are generated per process and are never
  printed or committed.
- Redis requires authentication.
- Mapped ports bind only to `127.0.0.1`; they are not intentionally exposed to
  the LAN.
- Images use explicit version tags rather than `latest`.
- The mock webhook must only receive synthetic test data—never production
  secrets or PII.
- The Docker daemon and its socket are trusted test infrastructure. Do not run
  unreviewed integration-test changes against a production Docker host.

Ryuk should remain enabled because it removes resources after interrupted test
runs. Set `TESTCONTAINERS_RYUK_DISABLED=true` only on a trusted, isolated host
where Ryuk cannot run and an external cleanup mechanism is guaranteed.

## Troubleshooting

An error connecting to the Docker API means the container runtime is not
running or is not accessible to the current user. On Windows, Docker Desktop's
WSL 2 backend also requires firmware virtualization and the Virtual Machine
Platform Windows feature.
