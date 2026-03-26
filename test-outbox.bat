@echo off
REM Test script for outbox pattern implementation (Windows)
REM This script should be run after Go is properly installed

echo Running Outbox Pattern Tests...

REM Clean dependencies
echo Cleaning dependencies...
go mod tidy
if %errorlevel% neq 0 (
    echo Failed to clean dependencies
    exit /b 1
)

REM Run unit tests with coverage
echo Running unit tests with coverage...
go test -v -cover ./internal/outbox/...
if %errorlevel% neq 0 (
    echo Unit tests failed
    exit /b 1
)

REM Run all tests with coverage report
echo Running all tests with coverage report...
go test -coverprofile=coverage.out ./...
if %errorlevel% neq 0 (
    echo Coverage test failed
    exit /b 1
)

REM Generate HTML coverage report
echo Generating HTML coverage report...
go tool cover -html=coverage.out -o coverage.html
if %errorlevel% neq 0 (
    echo Failed to generate coverage report
    exit /b 1
)

REM Run benchmarks
echo Running benchmarks...
go test -bench=. ./internal/outbox/...
if %errorlevel% neq 0 (
    echo Benchmark tests failed
    exit /b 1
)

REM Run race condition tests
echo Running race condition tests...
go test -race ./internal/outbox/...
if %errorlevel% neq 0 (
    echo Race condition tests failed
    exit /b 1
)

echo All tests completed successfully!
echo Coverage report available at: coverage.html
pause
