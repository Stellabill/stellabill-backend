#!/bin/bash

# Test script for outbox pattern implementation
# This script should be run after Go is properly installed

echo "Running Outbox Pattern Tests..."

# Clean dependencies
echo "Cleaning dependencies..."
go mod tidy

# Run unit tests with coverage
echo "Running unit tests with coverage..."
go test -v -cover ./internal/outbox/...

# Run integration tests (requires database)
echo "Running integration tests..."
go test -v -tags=integration ./internal/outbox/...

# Run all tests with coverage report
echo "Running all tests with coverage report..."
go test -coverprofile=coverage.out ./...

# Generate HTML coverage report
echo "Generating HTML coverage report..."
go tool cover -html=coverage.out -o coverage.html

# Check coverage threshold
echo "Checking coverage threshold..."
COVERAGE=$(go test -cover ./internal/outbox/... | grep "coverage:" | tail -1 | grep -o "[0-9.]*%" | tr -d '%')
THRESHOLD=95

if (( $(echo "$COVERAGE >= $THRESHOLD" | bc -l) )); then
    echo "✅ Coverage $COVERAGE% meets threshold of $THRESHOLD%"
else
    echo "❌ Coverage $COVERAGE% is below threshold of $THRESHOLD%"
    exit 1
fi

# Run benchmarks
echo "Running benchmarks..."
go test -bench=. ./internal/outbox/...

# Run race condition tests
echo "Running race condition tests..."
go test -race ./internal/outbox/...

# Run memory sanitizer tests
echo "Running memory sanitizer tests..."
go test -msan ./internal/outbox/...

echo "All tests completed successfully!"
echo "Coverage report available at: coverage.html"
