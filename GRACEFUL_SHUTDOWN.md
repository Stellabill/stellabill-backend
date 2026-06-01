# Graceful HTTP Server Shutdown

The HTTP server in `cmd/server/main.go` handles `SIGINT` and `SIGTERM` with a bounded graceful shutdown path.

## Runtime Behavior

1. The server starts `ListenAndServe` in a goroutine.
2. The main goroutine waits for either a startup/server error or a shutdown signal.
3. On the first `SIGINT` or `SIGTERM`, the server calls `http.Server.Shutdown` with a 30 second timeout.
4. In-flight requests are allowed to drain until the timeout expires.
5. Route-level cleanup runs with the same bounded context:
   - database pool close
   - OpenTelemetry tracer shutdown/flush
6. If graceful shutdown fails or times out, the server forces `Close` and exits non-zero.
7. A second signal during graceful shutdown forces an immediate close and exits non-zero.

## Cleanup Ownership

`internal/routes.RegisterWithCleanup` configures routes and returns a cleanup callback for resources created during route wiring. The existing `routes.Register` remains available for tests and tools that do not own process shutdown.

The current server wiring does not start an outbox worker from `cmd/server/main.go`; when that worker is wired into the HTTP process, its `Stop` method should be added to the cleanup callback so it participates in the same bounded shutdown path.

## Validation Notes

The server lifecycle is covered by `cmd/server/main_test.go` for:

- signal received before any request
- in-flight request draining
- shutdown timeout exceeded
- second signal forcing immediate close

Run verification with:

```sh
go test ./...
```
