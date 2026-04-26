# Health Checks & Dependency Monitoring

## Overview

The health check system provides three endpoints for monitoring stellabill-backend availability and dependency health status. These endpoints are designed to integrate with Kubernetes liveness/readiness probes and operational dashboards.

### Design Principles

1. **Non-cascading failures**: Liveness probe never fails due to dependency issues (app must be restarted, not due to slow DB)
2. **Graceful degradation**: Readiness probe signals when to temporarily route traffic away
3. **Observable**: All dependency statuses are visible to operators
4. **Secure**: No sensitive information (credentials, connection strings) in responses
5. **Efficient**: Timeouts prevent health checks from hanging; lightweight operations

## Endpoints

### 1. Liveness Probe (`/health/live`)

**Purpose**: Indicates if the application process is alive and responsive.

**Status Codes**:
- `200 OK` - Application is running

**Response**:
```json
{
  "status": "healthy",
  "service": "stellarbill-backend",
  "timestamp": "2026-04-23T10:30:45Z"
}
```

**Usage**: Configure Kubernetes liveness probe:
```yaml
livenessProbe:
  httpGet:
    path: /health/live
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 10
  timeoutSeconds: 5
  failureThreshold: 3
```

**Behavior**:
- Always returns 200 if the app is running
- Does NOT check dependencies (never cascades failures to external systems)
- Fails only if the application itself is unreachable (network down, port closed, etc.)

---

### 2. Readiness Probe (`/health/ready`)

**Purpose**: Indicates if the service is ready to accept requests.

**Status Codes**:
- `200 OK` - All critical dependencies are healthy
- `503 Service Unavailable` - One or more dependencies are degraded/unhealthy

**Response**:
```json
{
  "status": "healthy",
  "service": "stellarbill-backend",
  "timestamp": "2026-04-23T10:30:45Z",
  "dependencies": {
    "database": {
      "status": "healthy",
      "latency": "1.2ms"
    },
    "outbox": {
      "status": "healthy",
      "latency": "0.8ms",
      "details": {
        "pending_messages": 42,
        "processed_today": 1000
      }
    }
  }
}
```

**Usage**: Configure Kubernetes readiness probe:
```yaml
readinessProbe:
  httpGet:
    path: /health/ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
  timeoutSeconds: 10
  failureThreshold: 2
```

**Behavior**:
- Each dependency check has a 3-second timeout
- Database check includes exponential backoff retry (max 2 attempts)
- If any dependency is degraded/unhealthy, returns 503 and out-of-service marker
- Kubernetes automatically removes unhealthy instances from load balancer
- Enables safer rolling deployments (old pods drain traffic before termination)

**Status Values**:
- `healthy` - Dependency is responding normally
- `degraded` - Dependency is slow or having issues but may recover
- `unhealthy` - Dependency is completely down
- `not_configured` - Dependency is disabled or not initialized
- `timeout` - Check exceeded time limit

---

### 3. Health Details (`/health` or `/health/detailed`)

**Purpose**: Provides comprehensive health information for operational dashboards and monitoring systems.

**Status Codes**:
- `200 OK` - Returns detailed status regardless of dependency state

**Response**:
```json
{
  "status": "degraded",
  "service": "stellarbill-backend",
  "timestamp": "2026-04-23T10:30:45Z",
  "version": "1.2.3",
  "dependencies": {
    "database": {
      "status": "degraded",
      "message": "database connection timeout - may be overloaded or network issue",
      "latency": "3002.1ms"
    },
    "outbox": {
      "status": "healthy",
      "details": {
        "pending_messages": 156,
        "processed_today": 5432,
        "last_processed": "2026-04-23T10:29:30Z"
      },
      "latency": "0.5ms"
    }
  }
}
```

**Usage**: Use in monitoring systems (Datadog, New Relic, Prometheus):
```promql
# Example Prometheus query
stellarbill_health_dependencies_status{dependency="database"} == 0  # healthy
```

**Behavior**:
- Returns 200 regardless of dependency status (operator visibility)
- Includes version information for deployment tracking
- Shows detailed metrics and error messages
- Useful for dashboards that need to show *why* service is degraded

---

## Dependency Health Checks

### Database (PostgreSQL)

**Check Details**:
- Method: `PingContext()` with timeout
- Timeout: 3 seconds per attempt
- Retries: 2 attempts with exponential backoff (100ms, 200ms delays)
- Total max time: ~6.4 seconds

**Failure Scenarios**:
| Error | Signal | Action |
|-------|--------|--------|
| Connection refused | degraded | Check network routing, verify DB is running |
| Auth failed | degraded | Verify DATABASE_URL and credentials |
| Timeout | degraded | DB may be overloaded; check CPU, connections, locks |
| Connection pool exhausted | degraded | Increase max connections or reduce concurrent requests |
| Not configured | not_configured | DATABASE_URL env var not set |

**Example Runbook**:
```
Problem: Database shows "timeout" status
1. Check DB CPU and memory: SELECT * FROM pg_stat_statements;
2. Count connections: SELECT count(*) FROM pg_stat_activity;
3. Kill slow queries: SELECT * FROM pg_stat_activity WHERE query_start < now() - interval '5 minutes';
4. Monitor replica lag if read-replica is used
```

### Outbox / Event Queue

**Check Details**:
- Method: `Health()` interface with timeout
- Timeout: 3 seconds
- Includes queue statistics (pending messages, daily throughput)

**Failure Scenarios**:
| Error | Signal | Action |
|-------|--------|--------|
| Processing error | degraded | Check worker logs for unhandled exceptions |
| Queue overflow | degraded | Worker may be too slow; check processing latency |
| Connection lost | degraded | Check message broker (RabbitMQ/Kafka) is accessible |
| Worker crashed | unhealthy | Restart worker process; check error logs |
| Not configured | not_configured | Outbox manager not initialized in startup |

**Example Runbook**:
```
Problem: Outbox shows "degraded" with 5000+ pending messages
1. Check worker processing rate: curl http://localhost:8080/health/detailed | jq '.dependencies.outbox.details'
2. Compare to normal throughput baseline
3. Check worker process CPU/memory usage
4. If worker is hung, restart pod: kubectl rollout restart deployment/stellarbill-backend
5. Monitor recovery: watch 'curl http://localhost:8080/health/detailed'
```

---

## Integration with Kubernetes

### Full Pod Lifecycle Configuration

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stellarbill-backend
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0  # Ensure no total outage during rolling update
  template:
    metadata:
      labels:
        app: stellarbill-backend
    spec:
      containers:
      - name: api
        image: stellarbill-backend:latest
        ports:
        - containerPort: 8080
          name: http
        
        # Liveness probe: restart if app hangs
        livenessProbe:
          httpGet:
            path: /health/live
            port: http
          initialDelaySeconds: 10
          periodSeconds: 10
          timeoutSeconds: 5
          failureThreshold: 3
        
        # Readiness probe: stop routing traffic if degraded
        readinessProbe:
          httpGet:
            path: /health/ready
            port: http
          initialDelaySeconds: 5
          periodSeconds: 5
          timeoutSeconds: 10
          failureThreshold: 2
        
        # Startup probe: allow extended startup time for migration
        startupProbe:
          httpGet:
            path: /health/live
            port: http
          periodSeconds: 10
          failureThreshold: 30  # 5 minutes max startup
      
      terminationGracePeriodSeconds: 30
```

### Rolling Deployment Behavior

With the above configuration:

1. **Old pod**: Readiness probe fails → removed from load balancer
2. **Drain window**: 10+ seconds for in-flight requests to complete
3. **New pod**: Starts up, liveness passes immediately
4. **Dependencies check**: Readiness probe waits for DB/queue readiness
5. **Traffic routes**: Once readiness passes, load balancer adds pod
6. **Graceful termination**: Old pod killed after drain window

---

## Security Considerations

### ✅ What Health Endpoints DO Expose

- Service status (up/down/degraded)
- Dependency names (database, outbox)
- Latency measurements
- Generic error messages ("connection timeout")
- Queue statistics (# pending, # processed)
- Application version

### ❌ What Health Endpoints DO NOT Expose

- Database credentials or connection strings
- Application secrets (API keys, JWT keys)
- Internal error details or stack traces
- User data or PII
- Infrastructure details (IP addresses, hostnames)

### Best Practices

1. **Restrict access**: Limit health endpoints to internal networks
   ```yaml
   # Kubernetes NetworkPolicy
   kind: NetworkPolicy
   metadata:
     name: restrict-health
   spec:
     podSelector: {}
     policyTypes:
     - Ingress
     ingress:
     - from:
       - namespaceSelector:
           matchLabels:
             name: monitoring
   ```

2. **Log access**: Monitor who requests health checks
   ```go
   // In middleware
   if c.Request.URL.Path == "/health" {
       logger.Debug("health check", zap.String("remote_addr", c.ClientIP()))
   }
   ```

3. **Mask error messages**: Generic messages in prod
   ```go
   if isProduction {
       message = "dependency unavailable"  // not "auth failed with user=alice"
   }
   ```

---

## Monitoring & Alerting

### Prometheus Metrics Export

Add to `/metrics` endpoint (optional):
```prometheus
# HELP stellarbill_health_dependency_status Dependency health status (1=healthy, 0=degraded)
# TYPE stellarbill_health_dependency_status gauge
stellarbill_health_dependency_status{dependency="database"} 1
stellarbill_health_dependency_status{dependency="outbox"} 1

# HELP stellarbill_health_dependency_latency Dependency check latency in seconds
# TYPE stellarbill_health_dependency_latency histogram
stellarbill_health_dependency_latency_bucket{dependency="database",le="0.001"} 55
stellarbill_health_dependency_latency_bucket{dependency="database",le="0.005"} 89
```

### Alert Rules

```yaml
# File: alerts.yaml (for Prometheus AlertManager)
groups:
- name: stellarbill_health
  rules:
  - alert: CriticalDependencyDown
    expr: stellarbill_health_dependency_status == 0
    for: 2m
    annotations:
      summary: "{{ $labels.dependency }} is down"
      runbook: "docs/ops/database-outage-runbook.md"
  
  - alert: DegradedHealthDuration
    expr: |
      (time() - health_check_last_healthy{service="stellarbill"}) > 600
    for: 5m
    annotations:
      summary: "Service degraded for 10+ minutes"
```

---

## Testing Health Checks

### Manual Testing

```bash
# Test liveness (always 200)
curl -v http://localhost:8080/health/live

# Test readiness (200 if ready, 503 if degraded)
curl -v http://localhost:8080/health/ready

# Test with details
curl -s http://localhost:8080/health | jq .
```

### Load Testing

```bash
# Simulate Kubernetes probe traffic
ab -t 60 -c 2 -n 600 http://localhost:8080/health/ready

# Monitor response times
watch 'curl -w "@format.txt" -o /dev/null http://localhost:8080/health/ready'
```

### Chaos Testing

```bash
# Simulate DB timeout
# 1. Use tc (traffic control) to add packet loss to DB port
tc qdisc add dev eth0 root netem loss 100%

# 2. Verify health endpoint reports degraded
curl http://localhost:8080/health/ready  # Expect 503

# 3. Remove tc rule
tc qdisc del dev eth0 root
```

---

## Code Examples

### Using Health in Client Code

```go
// application/server.go
import "stellarbill-backend/internal/handlers"

func setupHealthChecks(router *gin.Engine, db *sql.DB, outbox handlers.OutboxHealther) {
    h := handlers.NewHandlerWithDependencies(
        planService,
        subscriptionService,
        db,  // Implements DBPinger interface
        outbox,
    )
    
    // Register endpoints
    router.GET("/health/live", h.LivenessProbe)
    router.GET("/health/ready", h.ReadinessProbe)
    router.GET("/health", h.HealthDetails)
}
```

### Dependency Interface Implementation

```go
// For database (already implemented by sql.DB)
var db *sql.DB
// db.PingContext() implements DBPinger automatically

// For outbox/queue
type CustomOutboxHealther struct {
    client *rabbitmq.Client
}

func (c *CustomOutboxHealther) Health() error {
    // Return nil if healthy, error otherwise
    return c.client.CheckHealth(context.Background())
}

func (c *CustomOutboxHealther) GetStats() (map[string]interface{}, error) {
    return map[string]interface{}{
        "pending_messages": c.client.QueueDepth(),
    }, nil
}
```

---

## Troubleshooting

### Health Endpoint Returns 503 After Deploy

**Cause**: Readiness check failing due to database migrations still running.

**Solution**:
1. Check startup logs: `kubectl logs -f pod/stellarbill-backend-xxx`
2. Watch readiness: `watch curl http://localhost:8080/health/ready`
3. If stuck, check DB migration status: `SELECT * FROM schema_migrations`
4. If migrations hung, may need to rollback and restart

### Readiness False Positives (503 despite healthy DB)

**Cause**: Check timeout was too aggressive; database was briefly slow.

**Solution**:
1. Increase check timeout in health.go (currently 10s total)
2. Add load test to baseline check times: `ab -n 1000 http://localhost:8080/health/ready`
3. Adjust `MaxDatabaseTimeout` const based on baseline + buffer

### Health Checks Causing High CPU (circular load)

**Cause**: Kubernetes or load balancer making too many requests to health endpoint.

**Solution**:
1. Reduce readiness probe frequency: `periodSeconds: 30` instead of 5
2. Or increase `failureThreshold` to tolerate brief failures: `failureThreshold: 5`
3. Monitor: `curl -w "%{time_total}\n" http://localhost:8080/health/ready`

---

## Future Enhancements

1. **Dependency-specific timeout tuning**: Allow per-dependency timeout config
2. **Weighted health**: Some dependencies critical, others non-critical
3. **Historical health data**: Expose trend data for better alerting
4. **Custom health checks**: Plugin system for app-specific checks
5. **Health check analytics**: Track check response times, patterns

---

## References

- Kubernetes Probes: https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/
- Graceful Shutdown: See [GRACEFUL_SHUTDOWN.md](GRACEFUL_SHUTDOWN.md)
- Outbox Pattern: See [docs/outbox-pattern.md](outbox-pattern.md)
- Previous Security Analysis: See [docs/security-analysis.md](security-analysis.md)
