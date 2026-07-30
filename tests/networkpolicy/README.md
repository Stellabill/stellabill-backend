# NetworkPolicy Test Suite

This directory contains the test infrastructure for validating Stellabill's Kubernetes NetworkPolicy implementation.

## Overview

The test suite:
1. Creates a kind (Kubernetes in Docker) cluster
2. Deploys all tiers (API, Worker, Database, Redis, Kafka) with proper labels
3. Installs NetworkPolicies via Helm
4. Runs synthetic connection tests to verify allowed/blocked traffic
5. Validates edge cases (DNS resolution, default-deny behavior)

## Prerequisites

Install the following tools:

- **kind**: https://kind.sigs.k8s.io/docs/user/quick-start/
  ```bash
  # macOS
  brew install kind
  
  # Linux
  curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
  chmod +x ./kind
  sudo mv ./kind /usr/local/bin/kind
  ```

- **kubectl**: https://kubernetes.io/docs/tasks/tools/
  ```bash
  # macOS
  brew install kubectl
  
  # Linux
  curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
  chmod +x kubectl
  sudo mv kubectl /usr/local/bin/kubectl
  ```

- **Helm**: https://helm.sh/docs/intro/install/
  ```bash
  # macOS
  brew install helm
  
  # Linux
  curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
  ```

- **Docker**: https://docs.docker.com/get-docker/

## Quick Start

Run the full test suite:

```bash
./test-netpol.sh
```

This will:
1. Create a 3-node kind cluster
2. Deploy test workloads (API, Worker, Database, Redis, Kafka)
3. Install NetworkPolicies
4. Run 8 connectivity tests
5. Display results summary
6. Prompt to keep or delete the cluster

Expected output:
```
[INFO] Running NetworkPolicy Validation Tests
[SUCCESS] ✅ DNS resolution works from API pod
[SUCCESS] ✅ API -> Database connection ALLOWED (expected)
[SUCCESS] ✅ API -> Redis connection ALLOWED (expected)
[SUCCESS] ✅ API -> Kafka connection BLOCKED (expected)
[SUCCESS] ✅ Worker -> Database connection ALLOWED (expected)
[SUCCESS] ✅ Worker -> Kafka connection ALLOWED (expected)
[SUCCESS] ✅ Worker -> Redis connection BLOCKED (expected)
[SUCCESS] ✅ Database egress BLOCKED (expected)

[INFO] Test Results Summary
[INFO] Total:  8
[SUCCESS] Passed: 8
[INFO] Failed: 0

[SUCCESS] 🎉 All NetworkPolicy tests passed!
```

## Test Cases

| Test | Source | Destination | Port | Expected | Rationale |
|------|--------|-------------|------|----------|-----------|
| DNS Resolution | API | kube-dns | 53 | Allow | Required for service discovery |
| API → Database | API | PostgreSQL | 5432 | Allow | API reads/writes subscription data |
| API → Redis | API | Redis | 6379 | Allow | Rate limiting and session cache |
| API → Kafka | API | Kafka | 9092 | **Deny** | API should not publish events directly |
| Worker → Database | Worker | PostgreSQL | 5432 | Allow | Worker reads/writes job state and outbox |
| Worker → Kafka | Worker | Kafka | 9092 | Allow | Worker publishes billing events |
| Worker → Redis | Worker | Redis | 6379 | **Deny** | Worker has no need for cache access |
| Database → API | Database | API | 8080 | **Deny** | Database cannot initiate outbound connections |

## Test Files

- **`kind-config.yaml`**: Kind cluster configuration (3 nodes, default CNI)
- **`test-deployments.yaml`**: Test workload manifests for all tiers
- **`test-matrix.yaml`**: netpol-cli test matrix (currently for reference)
- **`test-netpol.sh`**: Main test script with synthetic connection tests

## Manual Testing

If you want to inspect the cluster manually:

```bash
# Run the script and choose NOT to delete the cluster at the end
./test-netpol.sh

# In another terminal, interact with the cluster
export KUBECONFIG="$(kind get kubeconfig --name stellabill-netpol-test)"

# List NetworkPolicies
kubectl get networkpolicies

# Describe a specific policy
kubectl describe networkpolicy stellabill-api-egress

# Get a shell in an API pod
kubectl exec -it $(kubectl get pod -l tier=api -o jsonpath='{.items[0].metadata.name}') -- /bin/bash

# Test connectivity manually
nc -zv stellabill-db.default.svc.cluster.local 5432  # Should succeed
nc -zv stellabill-kafka.default.svc.cluster.local 9092  # Should timeout
```

## Cleanup

Delete the test cluster:

```bash
./test-netpol.sh cleanup
```

Or manually:

```bash
kind delete cluster --name stellabill-netpol-test
```

## Troubleshooting

### Test failures

**Symptom**: "API -> Database connection BLOCKED (unexpected)"

**Possible causes**:
1. Pod labels don't match NetworkPolicy selectors
   - Check: `kubectl get pods --show-labels`
2. NetworkPolicy not applied
   - Check: `kubectl get networkpolicies`
   - Describe: `kubectl describe networkpolicy stellabill-api-egress`
3. CNI plugin doesn't support NetworkPolicy
   - kind's default CNI (kindnet) supports NetworkPolicy
   - If using a custom CNI, ensure it has NetworkPolicy support

**Symptom**: All tests fail with timeouts

**Possible causes**:
1. Cluster not fully ready
   - Wait longer after deployment: `kubectl wait --for=condition=Available deployment --all --timeout=300s`
2. DNS not working
   - Check CoreDNS: `kubectl get pods -n kube-system -l k8s-app=kube-dns`

### Kind cluster issues

**Symptom**: "Cannot connect to Docker daemon"

**Solution**: Start Docker Desktop or the Docker daemon

**Symptom**: "Cluster creation takes too long"

**Solution**: 
- Check Docker resources (CPU, memory)
- Try with a single-node cluster: edit `kind-config.yaml` to remove worker nodes

## Integration with CI/CD

Example GitHub Actions workflow:

```yaml
name: NetworkPolicy Tests

on:
  pull_request:
    paths:
      - 'deploy/helm/stellabill/**'
      - 'tests/networkpolicy/**'

jobs:
  netpol-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Install kind
        run: |
          curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.20.0/kind-linux-amd64
          chmod +x ./kind
          sudo mv ./kind /usr/local/bin/kind
      
      - name: Install kubectl
        run: |
          curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
          chmod +x kubectl
          sudo mv kubectl /usr/local/bin/kubectl
      
      - name: Install helm
        run: |
          curl https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3 | bash
      
      - name: Run NetworkPolicy tests
        run: |
          cd tests/networkpolicy
          ./test-netpol.sh
```

## Advanced: Using netpol-cli

While the current test suite uses synthetic connection tests with `nc`, you can also use `netpol-cli` for more advanced validation:

```bash
# Install netpol-cli
go install github.com/mattfenwick/cyclonus/cmd/netpol-cli@latest

# Run validation against the test matrix
netpol-cli verify ./test-matrix.yaml
```

## Security Notes

- **Test deployments use simple passwords**: `test-deployments.yaml` contains hardcoded credentials for testing only. Never use these in production.
- **Pods run as root**: Test pods use `nicolaka/netshoot` which runs as root for network diagnostics. Production pods should use non-root users.
- **No TLS**: Test deployments use unencrypted connections. Production should use TLS/mTLS.

## References

- [Kubernetes NetworkPolicy Documentation](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [kind Documentation](https://kind.sigs.k8s.io/)
- [netpol-cli](https://github.com/mattfenwick/cyclonus)
- [Main NetworkPolicy Documentation](../../deploy/helm/stellabill/NETWORKPOLICY.md)
