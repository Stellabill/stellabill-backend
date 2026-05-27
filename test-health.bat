@echo off
REM Test script for health check implementation (Windows)
REM Run all health-related tests with coverage

setlocal enabledelayedexpansion

echo.
echo ================================
echo Health Check Test Suite (Windows)
echo ================================
echo.

REM Run liveness/readiness probe tests
echo Running probe tests...
go test ./internal/handlers -v -run "TestLiveness|TestReadiness|TestHealth" -timeout 30s
if !errorlevel! neq 0 (
    echo Test failed!
    exit /b 1
)

echo.
echo Running dependency health check tests...
go test ./internal/handlers -v -run "TestCheckDatabase|TestCheckOutbox" -timeout 30s
if !errorlevel! neq 0 (
    echo Test failed!
    exit /b 1
)

echo.
echo Running status logic tests...
go test ./internal/handlers -v -run "TestDeriveOverallStatus" -timeout 10s
if !errorlevel! neq 0 (
    echo Test failed!
    exit /b 1
)

echo.
echo Running concurrency tests...
go test ./internal/handlers -v -run "TestCheckAllDependencies" -timeout 30s
if !errorlevel! neq 0 (
    echo Test failed!
    exit /b 1
)

echo.
echo Running security tests...
go test ./internal/handlers -v -run "TestSecurityNoSensitiveData" -timeout 10s
if !errorlevel! neq 0 (
    echo Test failed!
    exit /b 1
)

echo.
echo Running integration tests...
go test ./internal/handlers -v -run "TestLifecycleEndpointsIntegration" -timeout 10s
if !errorlevel! neq 0 (
    echo Test failed!
    exit /b 1
)

echo.
echo ================================
echo Full test suite with coverage...
echo ================================
echo.

REM Run all handler tests with coverage
go test ./internal/handlers/... -v -cover -coverprofile=health-coverage.out
if !errorlevel! neq 0 (
    echo Test failed!
    exit /b 1
)

echo.
echo ================================
echo Coverage Report
echo ================================
go tool cover -func=health-coverage.out | find "health.go"

echo.
echo All tests passed!
echo.
echo Optional: View detailed coverage report
echo   go tool cover -html=health-coverage.out
