#!/bin/bash
# Test script for health check implementation
# Run all health-related tests with coverage

set -e

echo "================================"
echo "Health Check Test Suite"
echo "================================"
echo ""

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Run liveness/readiness probe tests
echo -e "${YELLOW}Running probe tests...${NC}"
go test ./internal/handlers -v -run "TestLiveness|TestReadiness|TestHealth" -timeout 30s

echo ""
echo -e "${YELLOW}Running dependency health check tests...${NC}"
go test ./internal/handlers -v -run "TestCheckDatabase|TestCheckOutbox" -timeout 30s

echo ""
echo -e "${YELLOW}Running status logic tests...${NC}"
go test ./internal/handlers -v -run "TestDeriveOverallStatus" -timeout 10s

echo ""
echo -e "${YELLOW}Running concurrency tests...${NC}"
go test ./internal/handlers -v -run "TestCheckAllDependencies" -timeout 30s

echo ""
echo -e "${YELLOW}Running security tests...${NC}"
go test ./internal/handlers -v -run "TestSecurityNoSensitiveData" -timeout 10s

echo ""
echo -e "${YELLOW}Running integration tests...${NC}"
go test ./internal/handlers -v -run "TestLifecycleEndpointsIntegration" -timeout 10s

echo ""
echo "================================"
echo -e "${YELLOW}Full test suite with coverage...${NC}"
echo "================================"
echo ""

# Run all handler tests with coverage
go test ./internal/handlers/... -v -cover -coverprofile=health-coverage.out

echo ""
echo "================================"
echo -e "${YELLOW}Coverage Report${NC}"
echo "================================"
go tool cover -func=health-coverage.out | grep "health.go"

echo ""
echo -e "${GREEN}✓ All tests passed!${NC}"
echo ""
echo "Optional: View detailed coverage report"
echo "  go tool cover -html=health-coverage.out"
