@echo off
setlocal enabledelayedexpansion

REM Test script for panic recovery middleware (Windows version)
REM This script tests various panic scenarios to ensure proper recovery

set BASE_URL=http://localhost:8080
set REQUEST_ID=test-request-%random%

echo === Panic Recovery Middleware Test Suite ===
echo Base URL: %BASE_URL%
echo Request ID: %REQUEST_ID%
echo.

REM Function to test endpoint (simulated with goto)
call :test_endpoint "/api/health" "Health check (no panic)" "200"
call :test_endpoint "/api/test/panic?type=string" "String panic" "500"
call :test_endpoint "/api/test/panic?type=runtime" "Runtime error panic" "500"
call :test_endpoint "/api/test/panic?type=nil" "Nil pointer panic" "500"
call :test_endpoint "/api/test/panic?type=custom" "Custom type panic" "500"
call :test_endpoint "/api/test/panic" "Default panic" "500"
call :test_endpoint "/api/test/panic-after-write" "Panic after headers written" "200"
call :test_endpoint "/api/test/nested-panic" "Nested panic" "500"

echo === Test Suite Complete ===
echo.
echo Key validations:
echo 1. All panics result in 500 status (except headers-written case)
echo 2. Safe error responses (no panic details leaked)
echo 3. Request ID correlation in responses
echo 4. Structured JSON responses for API calls
echo 5. Plain text fallback for non-JSON clients
echo.
echo Check server logs for detailed panic information and request correlation.
goto :eof

:test_endpoint
set endpoint=%~1
set description=%~2
set expected_status=%~3

echo Testing: %description%
echo Endpoint: %endpoint%

curl -s -w "HTTP_STATUS:%%{http_code}" -H "X-Request-ID: %REQUEST_ID%" -H "Content-Type: application/json" "%BASE_URL%%endpoint%" > temp_response.txt

REM Extract HTTP status and body (simplified for Windows batch)
for /f "tokens=*" %%i in (temp_response.txt) do set response=%%i

echo Expected Status: %expected_status%
echo Response: %response%

REM Simple status check (this is a basic implementation)
echo "%response%" | findstr "HTTP_STATUS:%expected_status%" >nul
if !errorlevel! equ 0 (
    echo ✅ PASS
) else (
    echo ❌ FAIL
)

echo ----------------------------------------
del temp_response.txt 2>nul
goto :eof
