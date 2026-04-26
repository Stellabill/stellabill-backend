# Security: Request Size Limits and Gzip Policy

## Overview

Issue #131 adds request size limits and gzip policy middleware to prevent memory abuse attacks in the Stellabill backend.

## Attack Vectors Mitigated

### 1. Request Size Exhaustion
**Risk**: Client sends extremely large request bodies to exhaust server memory.

**Mitigation**: `RequestSizeLimit` middleware enforces maximum request body size (default 10MB) before any parsing occurs. The body is read into memory once with a limited reader; if the limit is exceeded, the request is rejected with HTTP 413.

### 2. Decompression Bombs (Zip Bombs)
**Risk**: Client sends a small gzip file that decompresses to enormous size (e.g., 1KB → 1TB), exhausting memory.

**Mitigation**: `GzipPolicy` middleware:
- Only accepts `gzip` encoding; rejects deflate, br, zstd, etc. with HTTP 406
- Enforces absolute size cap on decompressed output (default 100MB)
- Enforces compression ratio limit (default 10:1) to catch bombs where compressed < 10MB but decompresses to > 100MB
- Uses `io.LimitReader` to abort reading if limits are exceeded

### 3. Memory Fragmentation via Chunked Encoding
**Risk**: Chunked transfer encoding with many small chunks can cause memory fragmentation.

**Mitigation**: `RequestSizeLimit` handles chunked bodies correctly by reading all chunks within the limit.

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `MAX_REQUEST_SIZE` | 10485760 (10MB) | Global max request body bytes |
| `MAX_GZIP_RATIO` | 10.0 | Max decompressed/compressed ratio |
| `MAX_GZIP_UNCOMPRESSED` | 104857600 (100MB) | Max decompressed bytes absolute cap |

## Per-Route Overrides

Routes needing different limits can attach middleware inline:

```go
api.POST("/upload", middleware.RequestSizeLimit(50<<20), handlers.UploadLargeFile)
api.POST("/small", middleware.RequestSizeLimit(1024), handlers.SmallPayload)
```

## Error Responses

**Request Too Large** (413):
```json
{"error":"request_too_large","max_bytes":10485760}
```

**Unsupported Encoding** (406):
```json
{"error":"unsupported_encoding","encoding":"deflate"}
```

**Decompression Bomb** (413):
```json
{"error":"decompression_bomb","decompressed_size":104857600,"max_uncompressed":104857600}
```

## Middleware Chain Order

Middleware is registered in `routes.go` BEFORE auth middleware to ensure limits are enforced first:

```
RequestSizeLimit → GzipPolicy → RateLimit → CORS → Auth
```

## Testing

See `internal/middleware/request_size_test.go` and `internal/middleware/gzip_policy_test.go` for edge case coverage including:
- Large payloads at and over limit
- Chunked transfer encoding
- Gzip over-limit decompression bombs
- Invalid gzip (truncated, non-gzip data)
- Per-route override scenarios
- Multiple sequential requests
