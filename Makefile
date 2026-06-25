.PHONY: loadtest-smoke

loadtest-smoke:
	@command -v k6 >/dev/null 2>&1 || { echo "k6 is required. Install from https://k6.io/docs/getting-started/installation/"; exit 1; }
	@echo "Starting local server on http://127.0.0.1:8080"
	@JWT_SECRET=${JWT_SECRET:-dev-secret} go run ./cmd/server >/tmp/loadtest-server.log 2>&1 & \
	SERVER_PID=$$!; \
	trap 'kill $$SERVER_PID >/dev/null 2>&1' EXIT; \
	sleep 4; \
	echo "Running load test smoke profile against ${LOADTEST_TARGET:-http://127.0.0.1:8080}"; \
	LOADTEST_TARGET=${LOADTEST_TARGET:-http://127.0.0.1:8080} JWT_SECRET=${JWT_SECRET:-dev-secret} k6 run --summary-export=./scripts/loadtest/plans-summary.json ./scripts/loadtest/plans.js; \
	LOADTEST_TARGET=${LOADTEST_TARGET:-http://127.0.0.1:8080} JWT_SECRET=${JWT_SECRET:-dev-secret} k6 run --summary-export=./scripts/loadtest/subscriptions-summary.json ./scripts/loadtest/subscriptions.js; \
	LOADTEST_TARGET=${LOADTEST_TARGET:-http://127.0.0.1:8080} JWT_SECRET=${JWT_SECRET:-dev-secret} k6 run --summary-export=./scripts/loadtest/statements-summary.json ./scripts/loadtest/statements.js

# Updates the golden snapshot files for JSON regression testing
.PHONY: update-golden
update-golden:
	go test ./internal/handlers/... -update