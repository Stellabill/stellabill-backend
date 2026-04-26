# Request Size Limits and Gzip Policy Middleware

## Overview

This document describes the request size limits and gzip policy middleware implemented for the Stellabill backend. These protections prevent memory abuse attacks by enforcing boundaries on incoming request payloads and decompression output.

## Features

### Request Size Limit

1. **Global Default**: Configurable maximum request body size (default 10MB)
2. **Per-Route Override**: Inline middleware for routes needing custom limits
3. **Pre-Parsing Enforcement**: Limits are checked before any JSON/body parsing
4. **Memory Efficiency**: Single read with `io.LimitReader`, body replaced for downstream handlers

### Gzip Policy

1. **Encoding Whitelist**: Only `gzip` accepted; all other encodings rejected
2. **Decompression Bomb Protection**: Absolute size cap on decompressed output
3. **Ratio Limiting**: Maximum decompressed/compressed ratio to catch edge-case bombs
4. **Early Abort**: Uses `io.LimitReader` to stop reading if limits exceeded

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `MAX_REQUEST_SIZE` | 10485760 (10MB) | Global max request body bytes |
| `MAX_GZIP_RATIO` | 10.0 | Max decompressed/compressed ratio |
| `MAX_GZIP_UNCOMPRESSED` | 104857600 (100MB) | Max decompressed bytes absolute cap |

### Configuration Examples

```bash
# Conservative limits for memory-constrained environments
MAX_REQUEST_SIZE=5242880       # 5MB
MAX_GZIP_RATIO=5.0
MAX_GZIP_UNCOMPRESSED=52428800 # 50MB

# Permissive limits for large file uploads
MAX_REQUEST_SIZE=104857600      # 100MB
MAX_GZIP_RATIO=20.0
MAX_GZIP_UNCOMPRESSED=1073741824 # 1GB
```

### Per-Route Override Pattern

```go
// Inline override for a specific route
api.POST("/upload/large", middleware.RequestSizeLimit(50<<20), handlers.UploadLargeFile)
api.POST("/upload/small", middleware.RequestSizeLimit(1024), handlers.SmallPayload)

// Override with custom gzip policy (e.g., larger decompressed limit for streaming)
api.POST("/stream", middleware.GzipPolicy(middleware.GzipPolicyConfig{
    MaxUncompressedBytes: 500 << 20,
    MaxRatio:             50.0,
}), handlers.StreamData)
```

## Implementation Details

### Request Size Limit Flow

```
1. Request arrives with body
2. Middleware reads body through io.LimitReader(maxBytes+1)
3. If read succeeds and len(body) <= maxBytes:
   - Replace c.Request.Body with bytes.NewBuffer(body)
   - Call c.Next() (handler parses body normally)
4. If len(body) > maxBytes:
   - Return 413 {"error":"request_too_large","max_bytes":N}
   - Do NOT call c.Next()
```

### Gzip Policy Flow

```
1. Check Content-Encoding header (lowercased, trimmed)
2. If empty or "identity": call c.Next()
3. If not "gzip": return 406 {"error":"unsupported_encoding","encoding":X}
4. Read entire body into memory
5. If compressedSize > MAX_GZIP_UNCOMPRESSED: return 413 (compressed over limit)
6. Create gzip.Reader on body bytes
7. Read with io.LimitReader(maxDestSize+1) where maxDestSize = min(ratioLimit, absoluteLimit)
8. If decompressed.Len() > maxDestSize: return 413 {"error":"decompression_bomb",...}
9. Replace c.Request.Body with decompressed buffer
10. Remove Content-Encoding header
11. Call c.Next()
```

### Middleware Chain Order

In `routes.go`, the order is:

```go
r.Use(middleware.RequestSizeLimit(cfg.MaxRequestSize))  // 1. Size limit first
r.Use(middleware.GzipPolicy(gzipCfg))                     // 2. Then gzip policy
r.Use(middleware.RateLimitMiddleware(rateLimitConfig))   // 3. Rate limiting
r.Use(cors.Middleware(corsProfile))                       // 4. CORS
r.Use(middleware.AuthMiddleware(jwtSecret))              // 5. Auth last
```

This ensures size limits are enforced **before** any body parsing occurs.

## Error Responses

### Request Too Large (413)

```json
{
  "error": "request_too_large",
  "max_bytes": 10485760
}
```

### Unsupported Encoding (406)

```json
{
  "error": "unsupported_encoding",
  "encoding": "deflate"
}
```

### Decompression Bomb (413)

```json
{
  "error": "decompression_bomb",
  "decompressed_size": 104857600,
  "max_uncompressed": 104857600,
  "compressed_size": 1024,
  "compression_ratio": 102400.0
}
```

## Security Considerations

### Memory Exhaustion Prevention

- **Pre-read enforcement**: Entire body must fit in memory to pass the limit check
- **No streaming parse**: JSON parsing happens after limit check passes
- **Body replacement**: Replaces `Request.Body` with in-memory buffer for downstream use

### Decompression Bomb Types Mitigated

1. **Ratio Bombs**: Small compressed file → huge decompressed output (e.g., 1KB → 1GB)
2. **Absolute Size Bombs**: Any decompressed output over absolute threshold
3. **Multi-layer Bombs**: gzip → deflate within gzip stream

### What Is NOT Mitigated

- **Custom encoding routes** requiring deflate/br/zstd (use separate endpoints)
- **Streaming decompression** (bodies are fully decompressed before handler)
- **Malformed-but-small payloads** (handled by JSON validation middleware)

## Testing

### Test Coverage

The implementation includes comprehensive tests covering:

- **Request Size Tests**: At limit, over limit, zero/negative limit (passthrough), empty body, chunked encoding
- **Gzip Tests**: Valid gzip, invalid gzip, truncated gzip, deflate/br rejection, mixed-case encoding
- **Bomb Tests**: Ratio bomb detection, absolute size bomb detection
- **Edge Cases**: Per-route overrides, multiple sequential requests, body re-read after limit check
- **Integration**: Handler receives correct body after middleware processes

### Running Tests

```bash
# Run all request size tests
go test ./internal/middleware/... -run TestRequestSizeLimit -v

# Run all gzip policy tests
go test ./internal/middleware/... -run TestGzipPolicy -v

# Run middleware tests with coverage
go test ./internal/middleware/... -cover

# Run specific test suites
go test ./internal/middleware/ -run "TestRequestSizeLimit_WithinLimit"
go test ./internal/middleware/ -run "TestGzipPolicy_ValidGzip"
```

## Troubleshooting

### Common Issues

1. **413 on legitimate large requests**: Increase `MAX_REQUEST_SIZE`
2. **406 on gzip requests**: Verify `Content-Encoding: gzip` header is sent correctly
3. **413 on small gzip decompression**: Adjust `MAX_GZIP_UNCOMPRESSED` or `MAX_GZIP_RATIO`
4. **Memory issues with large uploads**: Decrease limits or implement streaming endpoint

### Debug Information

```bash
# Enable Gin debug mode
GIN_MODE=debug

# Test request size limit
curl -X POST http://localhost:8080/api/endpoint \
  -H "Content-Type: application/json" \
  -d '{"data":"test"}'

# Test gzip rejection (should return 406)
curl -X POST http://localhost:8080/api/endpoint \
  -H "Content-Encoding: deflate" \
  -d 'test'
```

## Future Enhancements

### Potential Improvements

1. **Streaming JSON Parse**: Support for chunked JSON parsing to avoid full body read
2. **Configurable Encodings**: Allowlist specific encodings per endpoint
3. **Metrics Integration**: Prometheus metrics for rejected requests and decompressed sizes
4. **Adaptive Limits**: Dynamic limit adjustment based on server memory pressure
5. **Streaming Decompression**: Process gzip in chunks for large file handling

### Extension Points

The middleware is designed to be extensible:

- **Custom Size Checkers**: Implement custom logic for route-specific limits
- **Response Formats**: Customizable error response formats
- **Encoding Handlers**: Pluggable handlers for additional encodings
- **Callback Hooks**: Integration points for monitoring and logging
