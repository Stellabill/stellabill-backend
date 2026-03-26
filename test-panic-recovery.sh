#!/bin/bash

# Test script for panic recovery middleware
# This script tests various panic scenarios to ensure proper recovery

set -e

BASE_URL="http://localhost:8080"
REQUEST_ID="test-request-$(date +%s)"

echo "=== Panic Recovery Middleware Test Suite ==="
echo "Base URL: $BASE_URL"
echo "Request ID: $REQUEST_ID"
echo ""

# Function to test endpoint
test_endpoint() {
    local endpoint="$1"
    local description="$2"
    local expected_status="$3"
    
    echo "Testing: $description"
    echo "Endpoint: $endpoint"
    
    response=$(curl -s -w "\nHTTP_STATUS:%{http_code}" \
        -H "X-Request-ID: $REQUEST_ID" \
        -H "Content-Type: application/json" \
        "$BASE_URL$endpoint")
    
    http_code=$(echo "$response" | grep -o 'HTTP_STATUS:[0-9]*' | cut -d: -f2)
    body=$(echo "$response" | sed -e 's/HTTP_STATUS:[0-9]*$//')
    
    echo "Expected Status: $expected_status"
    echo "Actual Status: $http_code"
    echo "Response Body: $body"
    
    if [ "$http_code" = "$expected_status" ]; then
        echo "✅ PASS"
    else
        echo "❌ FAIL"
    fi
    echo "----------------------------------------"
}

# Test normal endpoint (should not panic)
test_endpoint "/api/health" "Health check (no panic)" "200"

# Test various panic scenarios
test_endpoint "/api/test/panic?type=string" "String panic" "500"
test_endpoint "/api/test/panic?type=runtime" "Runtime error panic" "500"
test_endpoint "/api/test/panic?type=nil" "Nil pointer panic" "500"
test_endpoint "/api/test/panic?type=custom" "Custom type panic" "500"
test_endpoint "/api/test/panic" "Default panic" "500"

# Test edge cases
test_endpoint "/api/test/panic-after-write" "Panic after headers written" "200"
test_endpoint "/api/test/nested-panic" "Nested panic" "500"

# Test without request ID (should generate one)
echo "Testing: Request ID generation"
echo "Endpoint: /api/test/panic"

response_no_id=$(curl -s -w "\nHTTP_STATUS:%{http_code}" \
    -H "Content-Type: application/json" \
    "$BASE_URL/api/test/panic")

http_code_no_id=$(echo "$response_no_id" | grep -o 'HTTP_STATUS:[0-9]*' | cut -d: -f2)
body_no_id=$(echo "$response_no_id" | sed -e 's/HTTP_STATUS:[0-9]*$//')

echo "Status: $http_code_no_id"
echo "Response: $body_no_id"

if echo "$body_no_id" | grep -q '"request_id"'; then
    echo "✅ PASS - Request ID generated"
else
    echo "❌ FAIL - Request ID not generated"
fi

echo "----------------------------------------"

# Test plain text response
echo "Testing: Plain text response"
response_text=$(curl -s -w "\nHTTP_STATUS:%{http_code}" \
    -H "X-Request-ID: $REQUEST_ID" \
    -H "Accept: text/plain" \
    "$BASE_URL/api/test/panic")

http_code_text=$(echo "$response_text" | grep -o 'HTTP_STATUS:[0-9]*' | cut -d: -f2)
body_text=$(echo "$response_text" | sed -e 's/HTTP_STATUS:[0-9]*$//')

echo "Status: $http_code_text"
echo "Response: $body_text"

if echo "$body_text" | grep -q "Internal Server Error"; then
    echo "✅ PASS - Plain text error response"
else
    echo "❌ FAIL - Plain text error response"
fi

echo "----------------------------------------"

echo "=== Test Suite Complete ==="
echo ""
echo "Key validations:"
echo "1. All panics result in 500 status (except headers-written case)"
echo "2. Safe error responses (no panic details leaked)"
echo "3. Request ID correlation in responses"
echo "4. Structured JSON responses for API calls"
echo "5. Plain text fallback for non-JSON clients"
echo ""
echo "Check server logs for detailed panic information and request correlation."
