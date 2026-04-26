# Health Checks - Quick Reference Card

## Three Endpoints

| Endpoint | Method | Status | Purpose | Checks |
|----------|--------|--------|---------|--------|
| `/health/live` | GET | 200 | K8s liveness (restart) | None (instant) |
| `/health/ready` | GET | 200/503 | K8s readiness (routing) | DB, Queue |
| `/health` | GET | 200 | Monitoring dashboard | DB, Queue, Stats |

---

## Quick Test

```bash
# Test all endpoints
curl http://localhost:8080/health/live     # → 200
curl http://localhost:8080/health/ready    # → 200 or 503
curl http://localhost:8080/health | jq .  # → Full details

# Run test suite
go test ./internal/handlers -v -cover      # → 16/16 pass
```

---

## Response Status Values

| Status | Meaning | Action |
|--------|---------|--------|
| `healthy` | ✅ All good | Continue normal operation |
| `degraded` | ⚠️ Issues detected | Readiness returns 503; check details |
| `unhealthy` | ❌ Down | Service unavailable |
| `not_configured` | ⏸️ Disabled | Dependency not initialized |
| `timeout` | ⏱️ Slow | Dependency exceeds timeout |

---

## Dependency Timeouts

| Dependency | Timeout | Retries | Total |
|------------|---------|---------|-------|
| Database | 3s each | 2x with backoff | ~6.4s |
| Queue | 3s | None | 3s |
| Overall | 10s | — | 10s |

---

## Status Derivation

```
All healthy    → Service healthy   → Readiness: 200
Any degraded   → Service degraded  → Readiness: 503
Any unhealthy  → Service unhealthy → Readiness: 503
```

---

## Integration in Code

```go
// In main.go
db := sql.Open("postgres", url)
outbox := outbox.NewManager(db)

h := handlers.NewHandlerWithDependencies(
    planSvc, subSvc,
    db,      // DBPinger
    outbox,  // OutboxHealther
)

router.GET("/health/live", h.LivenessProbe)
router.GET("/health/ready", h.ReadinessProbe)
router.GET("/health", h.HealthDetails)
```

---

## Kubernetes Deployment

```yaml
livenessProbe:
  httpGet: {path: /health/live, port: 8080}
  periodSeconds: 10
  failureThreshold: 3

readinessProbe:
  httpGet: {path: /health/ready, port: 8080}
  periodSeconds: 5
  failureThreshold: 2
```

---

## Troubleshooting

| Problem | Check | Fix |
|---------|-------|-----|
| Readiness stuck 503 | `/health` response | Check DB/queue health |
| Liveness keeps restarting | App logs | Fix application error |
| Slow health endpoint | Response latency | Adjust timeouts or check DB |
| Security warning | Response body | No secrets should appear |

---

## Files Reference

| File | Purpose |
|------|---------|
| `internal/handlers/health.go` | Implementation |
| `internal/handlers/health_test.go` | Tests (16 cases) |
| `docs/HEALTH_CHECKS.md` | Full operations guide |
| `docs/HEALTH_INTEGRATION_EXAMPLE.md` | Integration examples |
| `TEST_EXECUTION_HEALTH.md` | Test guide |
| `GIT_COMMIT_GUIDE.md` | Commit instructions |

---

## Test Execution

```bash
# All tests
go test ./internal/handlers -v

# Just health tests
go test ./internal/handlers -v -run Test.*Health

# With coverage
go test ./internal/handlers -cover

# Performance check
go test -race ./internal/handlers
```

---

## Security Checklist

- ✅ No DB credentials in response
- ✅ No API keys or tokens
- ✅ No stack traces
- ✅ Generic error messages
- ✅ No hostname/IP exposure

---

## API Response Examples

### Liveness (Always 200)
```json
{
  "status": "healthy",
  "service": "stellarbill-backend",
  "timestamp": "2026-04-23T10:30:45Z"
}
```

### Readiness (200 or 503)
```json
{
  "status": "healthy",
  "dependencies": {
    "database": {"status": "healthy", "latency": "1.2ms"},
    "outbox": {"status": "healthy", "latency": "0.8ms"}
  }
}
```

### Degraded
```json
{
  "status": "degraded",
  "dependencies": {
    "database": {
      "status": "degraded",
      "message": "connection timeout",
      "latency": "3002.1ms"
    }
  }
}
```

---

## Environment Variables

| Variable | Purpose | Example |
|----------|---------|---------|
| `DATABASE_URL` | DB connection | `postgres://user:pwd@host/db` |
| `VERSION` | App version (optional) | `1.2.3` |

---

## Constants

```go
StatusHealthy      = "healthy"
StatusDegraded     = "degraded"
StatusUnhealthy    = "unhealthy"

MaxRetries              = 2
InitialBackoff          = 100 * time.Millisecond
MaxDatabaseTimeout      = 3 * time.Second
MaxReadinessProbeTime   = 10 * time.Second
```

---

## Performance

| Operation | Typical Latency |
|-----------|-----------------|
| Liveness probe | <1ms |
| Readiness (healthy) | 2-10ms |
| Readiness (timeout) | 10s (context timeout) |
| Database ping | 1-2ms |
| Queue check | 0.5-1ms |

---

## Rolling Update Timeline

```
T=0:   Old pod: readiness check fails
T=1:   Old pod: removed from load balancer
T=5:   New pod: liveness passes, starts
T=7:   New pod: readiness checks dependencies
T=10:  New pod: dependencies ready, readiness passes
T=12:  New pod: added to load balancer
T=30:  Old pod: gracefully terminated
```

---

## Common Issues

### Readiness stuck 503
```bash
# Check what's down
curl http://localhost:8080/health | jq '.dependencies'

# Restart affected service
# e.g., kubectl rollout restart deployment/postgres
```

### Connection timeout
- DB overloaded → increase timeout or reduce connections
- Network issue → check connectivity
- Replica lag → check replication status

### Queue backlog growing
- Worker slow → check worker logs
- Processing errors → check error queue
- Restart worker → kubectl rollout restart deployment

---

## Next Actions

1. **Immediate**: `go test ./internal/handlers -v`
2. **Before commit**: Verify security test passes
3. **After commit**: Update main.go with health route registration
4. **Before deploy**: Configure Kubernetes probes
5. **During deployment**: Monitor `/health/ready` endpoints

---

## Documentation Links

- Full guide: `docs/HEALTH_CHECKS.md`
- Integration: `docs/HEALTH_INTEGRATION_EXAMPLE.md`
- Tests: `TEST_EXECUTION_HEALTH.md`
- Commit: `GIT_COMMIT_GUIDE.md`

---

**Print this card and keep it handy during development and deployment!**
