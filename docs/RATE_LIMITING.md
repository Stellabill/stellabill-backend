# API Rate Limiting Middleware

## Overview

This document describes the API rate limiting middleware implemented for the Stellarbill backend. The middleware provides configurable rate limiting with burst controls to protect service availability and reduce abuse.

## Features

### Rate Limiting Strategies

1. **Token Bucket Algorithm**: Implements the token bucket rate limiting algorithm with configurable refill rates and burst capacity.

2. **Multiple Modes**:
   - **IP Mode**: Rate limits by client IP address
   - **User Mode**: Rate limits by authenticated user ID (falls back to IP for anonymous requests)
   - **Hybrid Mode**: Rate limits by combination of user ID and IP address (most restrictive)

3. **Burst Control**: Allows temporary bursts of requests up to a configurable limit, then enforces sustained rate limits.

4. **Path Whitelisting**: Configurable paths that bypass rate limiting (e.g., health checks).

5. **Standardized Responses**: Consistent HTTP 429 responses with retry information.

## Configuration

### Environment Variables

| Variable               | Default       | Description                                                              |
| ---------------------- | ------------- | ------------------------------------------------------------------------ |
| `RATE_LIMIT_ENABLED`   | `true`        | Enable/disable rate limiting (enabled by default for security)           |
| `RATE_LIMIT_MODE`      | `ip`          | Rate limiting mode: `ip`, `user`, `hybrid`                               |
| `RATE_LIMIT_RPS`       | `10`          | Base requests per second (conservative default for security)             |
| `RATE_LIMIT_BURST`     | `20`          | Maximum burst size (2x RPS by default)                                   |
| `RATE_LIMIT_WHITELIST` | `/api/health` | Comma-separated list of whitelisted paths (only health check by default) |

### Per-Route Configuration

The middleware supports per-route rate limit overrides for sensitive endpoints. This allows applying stricter limits to high-cost or security-sensitive operations while maintaining reasonable limits for general API usage.

#### Default Per-Route Limits

The following endpoints have stricter rate limits by default:

- **List endpoints** (`/api/plans`, `/api/subscriptions`): 5 RPS, burst of 10
- **Reconciliation endpoint** (`/api/admin/reconcile`): 2 RPS, burst of 5

These limits are configured in `internal/routes/routes.go` and can be adjusted based on your security requirements and infrastructure capacity.

#### Configuring Per-Route Limits

Per-route limits are configured in the `RouteConfigs` map when initializing the rate limiter:

```go
rateLimitConfig := middleware.RateLimiterConfig{
    Enabled:        true,
    Mode:           ModeIP,
    RequestsPerSec: 10,  // Default limit
    BurstSize:      20,  // Default burst
    RouteConfigs: map[string]RouteSpecificConfig{
        "/api/sensitive":  {RequestsPerSec: 2, BurstSize: 5},
        "/api/expensive":  {RequestsPerSec: 5, BurstSize: 10},
    },
}
```

### Configuration Examples

```bash
# Basic IP-based rate limiting
RATE_LIMIT_ENABLED=true
RATE_LIMIT_MODE=ip
RATE_LIMIT_RPS=100
RATE_LIMIT_BURST=200

# User-based rate limiting for authenticated API
RATE_LIMIT_ENABLED=true
RATE_LIMIT_MODE=user
RATE_LIMIT_RPS=50
RATE_LIMIT_BURST=100

# Hybrid mode for high-security endpoints
RATE_LIMIT_ENABLED=true
RATE_LIMIT_MODE=hybrid
RATE_LIMIT_RPS=30
RATE_LIMIT_BURST=60
RATE_LIMIT_WHITELIST=/api/health,/api/status
```

## Implementation Details

### Token Bucket Algorithm

The token bucket algorithm works as follows:

1. **Initial State**: Each bucket starts with burst capacity tokens
2. **Refill Rate**: Tokens are added at a constant rate (requests per second)
3. **Request Processing**: Each request consumes one token
4. **Burst Handling**: Allows temporary bursts up to burst capacity
5. **Rate Limiting**: When empty, requests are rejected until tokens refill

### IP Address Extraction

The middleware extracts client IP addresses in the following priority order:

1. **X-Forwarded-For**: Takes the first IP from the comma-separated list
2. **X-Real-IP**: Uses the value if X-Forwarded-For is not present
3. **RemoteAddr**: Falls back to the direct connection IP

This approach properly handles requests through proxies and load balancers.

### Response Headers

The middleware adds rate limit information to response headers:

- `X-RateLimit-Limit`: Maximum requests allowed in the current window
- `X-RateLimit-Remaining`: Number of requests remaining in the current window
- `X-RateLimit-Reset`: Time when the rate limit window resets (RFC3339 format)
- `Retry-After`: Seconds to wait before retrying (only on rate-limited responses)

### Logging and Observability

The middleware provides logging capabilities for security monitoring:

- **Rate Limit Hit Logging**: When enabled, logs rate limit violations with path, client key, and mode
- **Configuration**: Set `LogRateLimitHits: true` in the rate limiter config
- **Log Format**: `[RATE_LIMIT] path=/api/endpoint key=192.168.1.100 mode=ip`

This logging helps detect:

- Brute force attack attempts
- Abusive client behavior
- Rate limit configuration tuning needs

Example log output:

```
[RATE_LIMIT] path=/api/admin/reconcile key=192.168.1.100 mode=ip
```

### Rate Limited Response

When rate limits are exceeded, the middleware returns:

```json
{
  "error": "rate limit exceeded",
  "code": "RATE_LIMIT_EXCEEDED",
  "message": "Too many requests. Please try again later."
}
```

## Security Considerations

### Memory Management

- **Automatic Cleanup**: Unused token buckets are automatically cleaned up after 10 minutes
- **Memory Efficiency**: Each client/user maintains only one token bucket
- **Goroutine Management**: Cleanup goroutines are properly managed to prevent leaks

### Attack Mitigation

1. **DoS Protection**: Prevents brute force attacks and API abuse
2. **Resource Conservation**: Limits server resource consumption
3. **Fair Usage**: Ensures equitable access among all clients

### Clock Drift Handling

The token bucket algorithm is resilient to minor clock drift:

- **Time-Based Refill**: Uses relative time differences for token refill
- **Grace Period**: Small timing variations don't significantly impact rate limiting
- **Consistent Behavior**: Rate limiting remains effective across server restarts

### Shared Proxy Considerations

When using rate limiting behind shared proxies:

1. **IP Mode**: All clients behind the same proxy share rate limits
2. **User Mode**: Authenticated users have individual rate limits
3. **Hybrid Mode**: Provides the most restrictive and fair limiting

## Performance Characteristics

### Memory Usage

- **Per Client**: ~100 bytes per active client/user
- **Cleanup**: Automatic removal of inactive buckets
- **Scalability**: Suitable for high-traffic applications

### CPU Overhead

- **Minimal Impact**: O(1) operations per request
- **Concurrent Safe**: Thread-safe implementation with mutex protection
- **Efficient Lookup**: Hash map-based client bucket lookup

## Testing

### Test Coverage

The implementation includes comprehensive tests covering:

- **Unit Tests**: Token bucket behavior and rate limiting logic
- **Integration Tests**: Middleware integration with Gin router
- **Edge Cases**: Malformed headers, clock drift, shared proxies
- **Concurrent Access**: Thread safety and race conditions
- **Memory Management**: Bucket cleanup and resource management

### Running Tests

```bash
# Run all rate limiting tests
go test ./internal/middleware/... -v

# Run tests with coverage
go test ./internal/middleware/... -cover

# Run specific test suites
go test ./internal/middleware/ -run TestTokenBucket
go test ./internal/middleware/ -run TestRateLimitMiddleware
```

## Best Practices

### Configuration Guidelines

1. **Start Conservative**: Begin with lower limits and monitor performance
2. **Monitor Usage**: Track rate limit violations and adjust as needed
3. **Differentiate Limits**: Use different limits for different user tiers
4. **Whitelist Critical Paths**: Ensure health checks and monitoring endpoints are accessible

### Deployment Considerations

1. **Staging Testing**: Test rate limits in staging before production
2. **Monitoring**: Monitor rate limit headers in client applications
3. **Logging**: Log rate limit violations for security analysis
4. **Documentation**: Document rate limits for API consumers

## Troubleshooting

### Common Issues

1. **Too Many Rate Limits**: Increase `RATE_LIMIT_RPS` or `RATE_LIMIT_BURST`
2. **Shared Proxy Issues**: Use `user` or `hybrid` mode instead of `ip`
3. **Memory Usage**: Monitor bucket cleanup and adjust intervals if needed
4. **Clock Synchronization**: Ensure server clocks are synchronized in clusters

### Debug Information

Enable debug logging to troubleshoot rate limiting issues:

```bash
# Enable debug mode
GIN_MODE=debug

# Monitor rate limit headers
curl -I http://localhost:8080/api/endpoint
```

## Future Enhancements

### Potential Improvements

1. **Redis Integration**: Distributed rate limiting for multi-server deployments
2. **Dynamic Configuration**: Runtime configuration updates
3. **Advanced Algorithms**: Sliding window or leaky bucket implementations
4. **Metrics Integration**: Prometheus metrics for rate limiting statistics
5. **Per-Endpoint Limits**: Different limits for different API endpoints

### Extension Points

The middleware is designed to be extensible:

- **Custom Key Generators**: Implement custom client identification logic
- **Storage Backends**: Pluggable storage for distributed deployments
- **Response Formats**: Customizable rate limit response formats
- **Callback Hooks**: Integration points for monitoring and logging
