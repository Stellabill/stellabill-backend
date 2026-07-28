# Stellabill NetworkPolicy - Connectivity Matrix

## Overview

This document describes the Kubernetes NetworkPolicy implementation for Stellabill backend. The policies enforce a **default-deny egress** model with explicit allowlist rules for required pod-to-pod communication, following the principle of least privilege.

## Security Goals

1. **Prevent lateral movement**: Block unauthorized pod-to-pod communication within the cluster
2. **Minimize attack surface**: Each tier can only communicate with explicitly allowed destinations
3. **Defense in depth**: Combine network-level controls with application-level authentication
4. **Audit compliance**: Provide clear documentation of all allowed traffic flows

## Architecture Tiers

| Tier | Component | Labels | Description |
|------|-----------|--------|-------------|
| API | `stellabill-api` | `tier: api`, `component: backend` | HTTP API server (Gin framework) |
| Worker | `stellabill-worker` | `tier: worker`, `component: background-jobs` | Background job processor with outbox pattern |
| Database | `stellabill-db` | `tier: database`, `component: postgresql` | PostgreSQL database |
| Cache | `stellabill-redis` | `tier: cache`, `component: redis` | Redis cache for rate limiting and sessions |
| Messaging | `stellabill-kafka` | `tier: messaging`, `component: kafka` | Kafka message broker for events |

## Connectivity Matrix

### Egress Rules (Outbound Traffic)

| Source Tier | Destination Tier | Protocol | Port | Purpose | Status |
|------------|------------------|----------|------|---------|--------|
| API | Database | TCP | 5432 | Read/write subscription, plan, customer data | ✅ Allowed |
| API | Redis | TCP | 6379 | Rate limiting, session cache | ✅ Allowed |
| API | Kafka | TCP | 9092 | N/A | ❌ Denied |
| API | DNS (kube-system) | UDP/TCP | 53 | Service name resolution | ✅ Allowed |
| Worker | Database | TCP | 5432 | Read/write job state, outbox events | ✅ Allowed |
| Worker | Kafka | TCP | 9092 | Publish billing events | ✅ Allowed |
| Worker | Redis | TCP | 6379 | N/A | ❌ Denied |
| Worker | DNS (kube-system) | UDP/TCP | 53 | Service name resolution | ✅ Allowed |
| Database | * | * | * | No outbound connections required | ❌ Denied (all) |
| Redis | * | * | * | No outbound connections required | ❌ Denied (all) |
| Kafka | * | * | * | No outbound connections required | ❌ Denied (all) |

### Ingress Rules (Inbound Traffic)

| Destination Tier | Source Tier | Protocol | Port | Status |
|-----------------|-------------|----------|------|--------|
| Database | API | TCP | 5432 | ✅ Allowed |
| Database | Worker | TCP | 5432 | ✅ Allowed |
| Database | * (other) | * | * | ❌ Denied |
| Redis | API | TCP | 6379 | ✅ Allowed |
| Redis | * (other) | * | * | ❌ Denied |
| Kafka | Worker | TCP | 9092 | ✅ Allowed |
| Kafka | * (other) | * | * | ❌ Denied |
| API | External ingress | TCP | 8080 | ✅ Allowed (via LoadBalancer/Ingress) |

## NetworkPolicy Resources

### 1. Default Deny Egress (`networkpolicy-default-deny.yaml`)

**Policy Name**: `stellabill-default-deny-egress`

**Scope**: All pods in the namespace

**Behavior**: Blocks all egress traffic by default. This is the foundation of our zero-trust model.

```yaml
spec:
  podSelector: {}  # Applies to all pods
  policyTypes:
    - Egress
  egress: []  # Empty = deny all
```

### 2. API Tier Egress (`networkpolicy-api.yaml`)

**Policy Name**: `stellabill-api-egress`

**Scope**: Pods with labels `tier: api, component: backend`

**Allowed Destinations**:
- DNS (kube-system namespace, UDP/TCP port 53)
- Database (PostgreSQL, TCP port 5432)
- Redis (TCP port 6379)

**Blocked**: Kafka, Worker, external internet

### 3. Worker Tier Egress (`networkpolicy-worker.yaml`)

**Policy Name**: `stellabill-worker-egress`

**Scope**: Pods with labels `tier: worker, component: background-jobs`

**Allowed Destinations**:
- DNS (kube-system namespace, UDP/TCP port 53)
- Database (PostgreSQL, TCP port 5432)
- Kafka (TCP port 9092)

**Blocked**: Redis, API, external internet

### 4. Ingress Policies (`networkpolicy-ingress.yaml`)

#### Database Ingress
**Policy Name**: `stellabill-database-ingress`

**Scope**: Pods with labels `tier: database, component: postgresql`

**Allowed Sources**: API tier, Worker tier (both on TCP port 5432)

#### Redis Ingress
**Policy Name**: `stellabill-redis-ingress`

**Scope**: Pods with labels `tier: cache, component: redis`

**Allowed Sources**: API tier only (TCP port 6379)

#### Kafka Ingress
**Policy Name**: `stellabill-kafka-ingress`

**Scope**: Pods with labels `tier: messaging, component: kafka`

**Allowed Sources**: Worker tier only (TCP port 9092)

## DNS Resolution

**Critical Requirement**: DNS must be explicitly allowed for cluster functionality.

All policies permit egress to:
- **Namespace**: `kube-system`
- **Pod Selector**: `k8s-app: kube-dns`
- **Ports**: UDP/TCP 53

This allows pods to resolve service names (e.g., `stellabill-db.default.svc.cluster.local`) to cluster IPs.

## Security Properties

### Threat Mitigation

| Threat | Mitigation |
|--------|------------|
| **Lateral Movement** | Default-deny blocks unauthorized pod-to-pod communication. Compromised API cannot reach Kafka. |
| **Database Breach** | Database cannot initiate outbound connections to exfiltrate data. |
| **Cache Poisoning** | Only API tier can write to Redis; Worker has no access. |
| **Event Injection** | Only Worker tier can publish to Kafka; API is blocked. |
| **DNS Tunneling** | DNS is allowed for functionality but can be monitored with network observability tools. |

### Defense in Depth Layers

1. **NetworkPolicy** (this implementation): L3/L4 network segmentation
2. **RBAC** (application level): JWT-based authentication and authorization
3. **Encryption**: TLS for external traffic, mTLS for internal recommended
4. **Audit Logging**: Application-level audit logs track sensitive operations

## Configuration

### Enabling/Disabling Policies

Edit `values.yaml`:

```yaml
networkPolicy:
  enabled: true  # Set to false to disable all policies
  defaultDenyEgress: true  # Set to false to disable default-deny
  allowDNS: true  # Always keep true unless using custom DNS
```

### Customizing Ports

Edit `values.yaml`:

```yaml
database:
  port: 5432  # Change if using non-standard port
redis:
  port: 6379
kafka:
  port: 9092
```

### Customizing DNS Configuration

Edit `values.yaml`:

```yaml
networkPolicy:
  dnsNamespace: kube-system  # Change if DNS is in different namespace
  dnsPort: 53
```

## Testing and Validation

See `tests/networkpolicy/` for:
- Kind cluster setup scripts
- `netpol-cli` validation tests
- Synthetic connection tests
- Edge case coverage (DNS, blocked paths, allowed paths)

### Quick Test

```bash
# Deploy to kind cluster
kind create cluster --name stellabill-netpol-test
helm install stellabill ./deploy/helm/stellabill

# Validate with netpol-cli
netpol-cli verify ./tests/networkpolicy/test-matrix.yaml
```

## Operational Considerations

### Monitoring

**Recommended Metrics**:
- Dropped packet count per policy (if CNI supports)
- Connection failure rate per service
- DNS query latency

**Tools**:
- **Cilium Hubble**: Flow visibility for NetworkPolicy enforcement
- **Calico Enterprise**: Policy ordering and troubleshooting
- **Prometheus**: Custom exporter for connection metrics

### Troubleshooting

**Symptom**: Service cannot reach dependency

**Steps**:
1. Verify pod labels match policy selectors: `kubectl get pods --show-labels`
2. Check NetworkPolicy is applied: `kubectl get networkpolicies`
3. Verify DNS resolution: `kubectl exec <pod> -- nslookup <service-name>`
4. Test connectivity: `kubectl exec <pod> -- nc -zv <service-name> <port>`
5. Check CNI logs for policy enforcement

**Common Issues**:
- Typo in pod labels → Policy does not apply
- Missing DNS egress rule → Cannot resolve service names
- CNI plugin does not support NetworkPolicy → Policies are ignored (check with `kubectl get networkpolicies`)

### Rollout Strategy

**Phase 1 - Audit Mode** (Week 1):
- Deploy policies with monitoring
- Collect metrics on blocked connections
- Identify legitimate traffic not in allowlist

**Phase 2 - Permissive Enforcement** (Week 2):
- Apply policies to staging environment
- Run full integration test suite
- Validate no regressions

**Phase 3 - Production Deployment** (Week 3):
- Deploy during low-traffic window
- Monitor error rates and latency
- Keep rollback plan ready

### Rollback

```bash
# Quick disable
helm upgrade stellabill ./deploy/helm/stellabill --set networkPolicy.enabled=false

# Or delete policies individually
kubectl delete networkpolicy stellabill-default-deny-egress
kubectl delete networkpolicy stellabill-api-egress
kubectl delete networkpolicy stellabill-worker-egress
kubectl delete networkpolicy stellabill-database-ingress
kubectl delete networkpolicy stellabill-redis-ingress
kubectl delete networkpolicy stellabill-kafka-ingress
```

## Compliance and Audit

**Frameworks**:
- **PCI-DSS**: Requirement 1.3.5 - Network segmentation for cardholder data environment
- **SOC 2**: CC6.6 - Network segmentation controls
- **NIST 800-53**: SC-7 - Boundary Protection

**Audit Evidence**:
- This document (connectivity matrix)
- NetworkPolicy YAML manifests
- Test results from `tests/networkpolicy/`
- Kubernetes audit logs showing policy creation/modification

## References

- [Kubernetes NetworkPolicy Documentation](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [NetworkPolicy Editor (Online Tool)](https://networkpolicy.io/)
- [Cilium NetworkPolicy Tutorial](https://docs.cilium.io/en/stable/security/policy/)
- [CNCF NetworkPolicy Best Practices](https://www.cncf.io/blog/2021/12/15/kubernetes-network-policy-best-practices/)

## Change History

| Date | Version | Author | Changes |
|------|---------|--------|---------|
| 2026-07-28 | 0.1.0 | DevOps Team | Initial NetworkPolicy implementation with default-deny egress |

## Questions and Support

For questions about this NetworkPolicy implementation:
- Create an issue in the repository
- Contact the DevOps team
- Refer to `tests/networkpolicy/README.md` for testing guidance
