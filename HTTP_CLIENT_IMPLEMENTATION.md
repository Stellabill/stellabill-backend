# Resilient HTTP Client Implementation

The `stellabill-backend` project implements a shared, resilient HTTP client in `internal/httpclient` to standardize outbound network communications. This wrapper prevents cascading failures, guards against retry storms, and ensures safe idempotency when making external API calls.

## Core Features

1. **Bounded Timeouts**: Enforces individual request timeouts to prevent hanging connections or partial read deadlocks (`RequestTimeout`).
2. **Jittered Exponential Backoff**: Uses an exponential backoff strategy with up to 20% random jitter to prevent "thundering herd" retry storms.
3. **Circuit Breaker Pattern**: Global circuit breakers (partitioned by target `host`) fast-fail requests if the upstream service experiences a high failure rate.
4. **Idempotency Guard**: Strictly prevents duplicate side effects by refusing to retry non-idempotent methods (`POST`, `PATCH`) unless an `Idempotency-Key` header is present.
5. **Rate-Limit Respect**: Automatically parses and respects the `Retry-After` header for `429 Too Many Requests` and `503 Service Unavailable` responses.

## Metrics Instrumentation (Prometheus)

All resilient HTTP calls automatically track operational health through Prometheus metrics defined in `internal/httpclient/metrics.go`.

| Metric Name | Type | Labels | Description |
| :--- | :--- | :--- | :--- |
| `http_client_retries_total` | Counter | `host`, `method` | Tracks the number of retry attempts triggered. |
| `http_client_failures_total` | Counter | `host`, `method`, `reason` | Tracks ultimate request failures. Reasons include `timeout`, `non_2xx`, `error`, and `max_retries_reached`. |
| `http_client_circuit_state` | Gauge | `host` | Tracks the live circuit breaker state: `0` (Closed), `1` (Open), `2` (Half-Open). |

## When to Retry vs. Fail Fast

Understanding the client's internal routing behavior is critical for safe integrations.

### When does the client Retry?
The client **will seamlessly retry** a request when:
- A transient network error occurs (e.g., DNS failure, connection reset).
- A request times out (respecting `RequestTimeout`).
- The upstream server returns a `5xx` status code (e.g., `500`, `502`, `504`).
- The upstream server returns a `429 Too Many Requests` or `503 Service Unavailable` (overriding the backoff with the `Retry-After` header if provided).

**Idempotency Constraint**: Retries are *only* executed if the HTTP method is intrinsically idempotent (`GET`, `PUT`, `DELETE`), OR if the caller explicitly provided an `Idempotency-Key` HTTP header. 

### When does the client Fail Fast?
The client **will instantly fail** and return an error without hitting the network when:
- **The Circuit is Open**: If the downstream `host` has breached the `maxFailures` threshold recently, the circuit breaker opens and returns `ErrCircuitOpen` immediately.
- **Non-Idempotent Constraint**: If a `POST` or `PATCH` request fails on the first attempt and lacks an `Idempotency-Key` header, the client aborts to avoid duplicate side effects (unless `RetryNonIdempotent` configuration is forcibly set to true).
- **Max Retries Reached**: Once `MaxRetries` attempts have been exhausted, the final response is returned to the caller.

## Usage Example

```go
// Initialize the client with the remote host and zap logger
logger := security.ProductionLogger()
client := httpclient.NewClient("api.external-service.com", logger)

// Prepare an idempotent POST request
req, _ := http.NewRequest(http.MethodPost, "https://api.external-service.com/v1/resource", body)
req.Header.Set("Idempotency-Key", "unique-event-id-123")

// Execute resiliently
resp, err := client.Do(req)
if err != nil {
    // Handle terminal network failure
}
defer resp.Body.Close()

// Handle response (check resp.StatusCode, etc.)
```
