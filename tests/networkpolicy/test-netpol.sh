#!/bin/bash
set -euo pipefail

# NetworkPolicy Test Suite for Stellabill
# This script sets up a kind cluster, deploys the app with NetworkPolicies,
# and validates connectivity using synthetic connection tests.

CLUSTER_NAME="stellabill-netpol-test"
NAMESPACE="default"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HELM_CHART_DIR="${SCRIPT_DIR}/../../deploy/helm/stellabill"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${BLUE}[INFO]${NC} $*"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $*"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $*"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $*"
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    if ! command -v kind &> /dev/null; then
        log_error "kind is not installed. Install from: https://kind.sigs.k8s.io/docs/user/quick-start/"
        exit 1
    fi
    
    if ! command -v kubectl &> /dev/null; then
        log_error "kubectl is not installed. Install from: https://kubernetes.io/docs/tasks/tools/"
        exit 1
    fi
    
    if ! command -v helm &> /dev/null; then
        log_error "helm is not installed. Install from: https://helm.sh/docs/intro/install/"
        exit 1
    fi
    
    log_success "All prerequisites are installed"
}

# Create kind cluster
create_cluster() {
    log_info "Creating kind cluster: ${CLUSTER_NAME}..."
    
    if kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
        log_warning "Cluster ${CLUSTER_NAME} already exists. Deleting..."
        kind delete cluster --name "${CLUSTER_NAME}"
    fi
    
    kind create cluster --name "${CLUSTER_NAME}" --config "${SCRIPT_DIR}/kind-config.yaml"
    
    # Wait for cluster to be ready
    log_info "Waiting for cluster to be ready..."
    kubectl wait --for=condition=Ready nodes --all --timeout=120s
    
    log_success "Cluster created successfully"
}

# Deploy test workloads
deploy_workloads() {
    log_info "Deploying test workloads..."
    
    kubectl apply -f "${SCRIPT_DIR}/test-deployments.yaml"
    
    # Wait for deployments to be ready
    log_info "Waiting for deployments to be ready..."
    kubectl wait --for=condition=Available deployment/stellabill-api --timeout=180s
    kubectl wait --for=condition=Available deployment/stellabill-worker --timeout=180s
    kubectl wait --for=condition=Available deployment/stellabill-db --timeout=180s
    kubectl wait --for=condition=Available deployment/stellabill-redis --timeout=180s
    kubectl wait --for=condition=Available deployment/stellabill-kafka --timeout=180s
    
    log_success "All workloads deployed and ready"
}

# Install NetworkPolicies via Helm
install_networkpolicies() {
    log_info "Installing NetworkPolicies via Helm..."
    
    helm install stellabill "${HELM_CHART_DIR}" \
        --namespace "${NAMESPACE}" \
        --set networkPolicy.enabled=true \
        --set networkPolicy.defaultDenyEgress=true
    
    # Wait a moment for policies to be applied
    sleep 5
    
    # List installed NetworkPolicies
    log_info "Installed NetworkPolicies:"
    kubectl get networkpolicies -n "${NAMESPACE}"
    
    log_success "NetworkPolicies installed"
}

# Test DNS resolution (should be allowed)
test_dns_resolution() {
    log_info "Testing DNS resolution from API pod..."
    
    local api_pod=$(kubectl get pod -l tier=api,component=backend -o jsonpath='{.items[0].metadata.name}')
    
    if kubectl exec "${api_pod}" -- nslookup stellabill-db.default.svc.cluster.local &> /dev/null; then
        log_success "✅ DNS resolution works from API pod"
        return 0
    else
        log_error "❌ DNS resolution FAILED from API pod"
        return 1
    fi
}

# Test allowed connection: API -> Database
test_api_to_database() {
    log_info "Testing API -> Database (should be ALLOWED)..."
    
    local api_pod=$(kubectl get pod -l tier=api,component=backend -o jsonpath='{.items[0].metadata.name}')
    local db_ip=$(kubectl get service stellabill-db -o jsonpath='{.spec.clusterIP}')
    
    if kubectl exec "${api_pod}" -- timeout 5 nc -zv "${db_ip}" 5432 &> /dev/null; then
        log_success "✅ API -> Database connection ALLOWED (expected)"
        return 0
    else
        log_error "❌ API -> Database connection BLOCKED (unexpected)"
        return 1
    fi
}

# Test allowed connection: API -> Redis
test_api_to_redis() {
    log_info "Testing API -> Redis (should be ALLOWED)..."
    
    local api_pod=$(kubectl get pod -l tier=api,component=backend -o jsonpath='{.items[0].metadata.name}')
    local redis_ip=$(kubectl get service stellabill-redis -o jsonpath='{.spec.clusterIP}')
    
    if kubectl exec "${api_pod}" -- timeout 5 nc -zv "${redis_ip}" 6379 &> /dev/null; then
        log_success "✅ API -> Redis connection ALLOWED (expected)"
        return 0
    else
        log_error "❌ API -> Redis connection BLOCKED (unexpected)"
        return 1
    fi
}

# Test blocked connection: API -> Kafka
test_api_to_kafka_blocked() {
    log_info "Testing API -> Kafka (should be BLOCKED)..."
    
    local api_pod=$(kubectl get pod -l tier=api,component=backend -o jsonpath='{.items[0].metadata.name}')
    local kafka_ip=$(kubectl get service stellabill-kafka -o jsonpath='{.spec.clusterIP}')
    
    # Connection should timeout/fail
    if kubectl exec "${api_pod}" -- timeout 5 nc -zv "${kafka_ip}" 9092 &> /dev/null; then
        log_error "❌ API -> Kafka connection ALLOWED (unexpected - should be blocked)"
        return 1
    else
        log_success "✅ API -> Kafka connection BLOCKED (expected)"
        return 0
    fi
}

# Test allowed connection: Worker -> Database
test_worker_to_database() {
    log_info "Testing Worker -> Database (should be ALLOWED)..."
    
    local worker_pod=$(kubectl get pod -l tier=worker,component=background-jobs -o jsonpath='{.items[0].metadata.name}')
    local db_ip=$(kubectl get service stellabill-db -o jsonpath='{.spec.clusterIP}')
    
    if kubectl exec "${worker_pod}" -- timeout 5 nc -zv "${db_ip}" 5432 &> /dev/null; then
        log_success "✅ Worker -> Database connection ALLOWED (expected)"
        return 0
    else
        log_error "❌ Worker -> Database connection BLOCKED (unexpected)"
        return 1
    fi
}

# Test allowed connection: Worker -> Kafka
test_worker_to_kafka() {
    log_info "Testing Worker -> Kafka (should be ALLOWED)..."
    
    local worker_pod=$(kubectl get pod -l tier=worker,component=background-jobs -o jsonpath='{.items[0].metadata.name}')
    local kafka_ip=$(kubectl get service stellabill-kafka -o jsonpath='{.spec.clusterIP}')
    
    if kubectl exec "${worker_pod}" -- timeout 5 nc -zv "${kafka_ip}" 9092 &> /dev/null; then
        log_success "✅ Worker -> Kafka connection ALLOWED (expected)"
        return 0
    else
        log_error "❌ Worker -> Kafka connection BLOCKED (unexpected)"
        return 1
    fi
}

# Test blocked connection: Worker -> Redis
test_worker_to_redis_blocked() {
    log_info "Testing Worker -> Redis (should be BLOCKED)..."
    
    local worker_pod=$(kubectl get pod -l tier=worker,component=background-jobs -o jsonpath='{.items[0].metadata.name}')
    local redis_ip=$(kubectl get service stellabill-redis -o jsonpath='{.spec.clusterIP}')
    
    # Connection should timeout/fail
    if kubectl exec "${worker_pod}" -- timeout 5 nc -zv "${redis_ip}" 6379 &> /dev/null; then
        log_error "❌ Worker -> Redis connection ALLOWED (unexpected - should be blocked)"
        return 1
    else
        log_success "✅ Worker -> Redis connection BLOCKED (expected)"
        return 0
    fi
}

# Test blocked egress: Database cannot initiate outbound connections
test_database_egress_blocked() {
    log_info "Testing Database -> API (should be BLOCKED)..."
    
    local db_pod=$(kubectl get pod -l tier=database,component=postgresql -o jsonpath='{.items[0].metadata.name}')
    local api_ip=$(kubectl get service stellabill-api -o jsonpath='{.spec.clusterIP}')
    
    # Install nc in postgres container if needed, or use psql
    # For this test, we'll check if the connection times out
    if kubectl exec "${db_pod}" -- timeout 5 sh -c "command -v nc && nc -zv ${api_ip} 8080" &> /dev/null; then
        log_error "❌ Database -> API connection ALLOWED (unexpected - should be blocked)"
        return 1
    else
        log_success "✅ Database egress BLOCKED (expected)"
        return 0
    fi
}

# Run all tests
run_all_tests() {
    log_info "=========================================="
    log_info "Running NetworkPolicy Validation Tests"
    log_info "=========================================="
    echo
    
    local total=0
    local passed=0
    local failed=0
    
    # Array of test functions
    tests=(
        "test_dns_resolution"
        "test_api_to_database"
        "test_api_to_redis"
        "test_api_to_kafka_blocked"
        "test_worker_to_database"
        "test_worker_to_kafka"
        "test_worker_to_redis_blocked"
        "test_database_egress_blocked"
    )
    
    for test in "${tests[@]}"; do
        total=$((total + 1))
        if $test; then
            passed=$((passed + 1))
        else
            failed=$((failed + 1))
        fi
        echo
    done
    
    log_info "=========================================="
    log_info "Test Results Summary"
    log_info "=========================================="
    log_info "Total:  ${total}"
    log_success "Passed: ${passed}"
    if [ $failed -gt 0 ]; then
        log_error "Failed: ${failed}"
    else
        log_info "Failed: ${failed}"
    fi
    echo
    
    if [ $failed -eq 0 ]; then
        log_success "🎉 All NetworkPolicy tests passed!"
        return 0
    else
        log_error "❌ Some tests failed. Please review the NetworkPolicy configuration."
        return 1
    fi
}

# Cleanup
cleanup() {
    log_info "Cleaning up..."
    
    read -p "Do you want to delete the test cluster? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        kind delete cluster --name "${CLUSTER_NAME}"
        log_success "Cluster deleted"
    else
        log_info "Cluster preserved for manual inspection"
        log_info "To delete later: kind delete cluster --name ${CLUSTER_NAME}"
    fi
}

# Main execution
main() {
    log_info "Starting NetworkPolicy Test Suite"
    echo
    
    check_prerequisites
    echo
    
    create_cluster
    echo
    
    deploy_workloads
    echo
    
    install_networkpolicies
    echo
    
    # Wait a bit for policies to fully propagate
    log_info "Waiting 10 seconds for policies to propagate..."
    sleep 10
    echo
    
    local test_result=0
    run_all_tests || test_result=$?
    echo
    
    # Show policy details
    log_info "NetworkPolicy Details:"
    kubectl describe networkpolicies -n "${NAMESPACE}"
    echo
    
    cleanup
    
    exit $test_result
}

# Handle script arguments
case "${1:-}" in
    cleanup)
        kind delete cluster --name "${CLUSTER_NAME}" || true
        log_success "Cleanup complete"
        ;;
    *)
        main
        ;;
esac
