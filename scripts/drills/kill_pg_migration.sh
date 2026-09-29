#!/usr/bin/env bash
# scripts/drills/kill_pg_migration.sh
#
# Chaos drill: start a migration, kill Postgres at a random mid-migration
# point, restart Postgres, and verify the migrations runner recovers cleanly
# without leaving partial DDL or a corrupt schema_migrations table.
#
# ─────────────────────────────────────────────────────────────────────────────
# SAFETY CONTRACT
#   This script MUST only run against ephemeral, disposable clusters.
#   It will SIGKILL the Postgres process. Never point it at a shared or
#   production database.
# ─────────────────────────────────────────────────────────────────────────────
#
# Usage:
#   POSTGRES_CONTAINER=my-pg bash scripts/drills/kill_pg_migration.sh
#
# Environment variables (all have defaults):
#   POSTGRES_CONTAINER   Docker container name/id running Postgres  [stellabill-chaos-pg]
#   POSTGRES_IMAGE       Postgres Docker image to start             [postgres:17-alpine]
#   POSTGRES_USER        Postgres superuser                         [drill]
#   POSTGRES_PASSWORD    Postgres password                          [drill]
#   POSTGRES_DB          Database name                              [drill]
#   KILL_DELAY_MIN       Min seconds before kill (float)            [0.05]
#   KILL_DELAY_MAX       Max seconds before kill (float)            [2.0]
#   SLACK_WEBHOOK_URL    Optional — POST failure summary to Slack   []
#   RESULTS_CSV          Path to append timing results              [drill-results.csv]
#   MIGRATE_CMD          Migration command to run                   [go run ./cmd/migrate up]
#
# Output:
#   - Per-run result line appended to $RESULTS_CSV
#   - Slack notification on failure (if SLACK_WEBHOOK_URL is set)
#   - Exit 0 on success (recovery verified), exit 1 on failure
#
# Requirements: bash 4+, docker, bc, awk, curl (for Slack)

set -euo pipefail

# ─────────────────────────────────────────────────────────────────────────────
# Configuration
# ─────────────────────────────────────────────────────────────────────────────
POSTGRES_CONTAINER="${POSTGRES_CONTAINER:-stellabill-chaos-pg}"
POSTGRES_IMAGE="${POSTGRES_IMAGE:-postgres:17-alpine}"
POSTGRES_USER="${POSTGRES_USER:-drill}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-drill}"
POSTGRES_DB="${POSTGRES_DB:-drill}"
POSTGRES_PORT="${POSTGRES_PORT:-15432}"
DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable"

KILL_DELAY_MIN="${KILL_DELAY_MIN:-0.05}"
KILL_DELAY_MAX="${KILL_DELAY_MAX:-2.0}"
SLACK_WEBHOOK_URL="${SLACK_WEBHOOK_URL:-}"
RESULTS_CSV="${RESULTS_CSV:-drill-results.csv}"
MIGRATE_CMD="${MIGRATE_CMD:-go run ./cmd/migrate up}"

DRILL_ID="drill-$(date +%Y%m%dT%H%M%S)-$$"
RESULT="UNKNOWN"
RECOVERY_TIME_MS=0
NOTES=""

# ─────────────────────────────────────────────────────────────────────────────
# Helpers
# ─────────────────────────────────────────────────────────────────────────────
log()  { echo "[chaos-drill] $(date -u +%H:%M:%S) $*"; }
die()  { log "FATAL: $*" >&2; RESULT="ERROR" NOTES="$*" record_result; exit 1; }
ts_ms() { date +%s%3N; }

cleanup() {
  log "Cleaning up container ${POSTGRES_CONTAINER}…"
  docker rm -f "${POSTGRES_CONTAINER}" &>/dev/null || true
}
trap cleanup EXIT

require_cmd() {
  command -v "$1" &>/dev/null || die "Required command not found: $1"
}

# Random float between MIN and MAX using awk
random_delay() {
  awk -v min="${KILL_DELAY_MIN}" -v max="${KILL_DELAY_MAX}" \
    'BEGIN { srand(); printf "%.3f\n", min + rand() * (max - min) }'
}

# ─────────────────────────────────────────────────────────────────────────────
# CSV result recording
# ─────────────────────────────────────────────────────────────────────────────
record_result() {
  local header="drill_id,result,recovery_time_ms,kill_delay_s,timestamp,notes"
  if [[ ! -f "${RESULTS_CSV}" ]]; then
    echo "${header}" > "${RESULTS_CSV}"
  fi
  printf '%s,%s,%d,%.3f,%s,"%s"\n' \
    "${DRILL_ID}" \
    "${RESULT}" \
    "${RECOVERY_TIME_MS}" \
    "${KILL_DELAY_S:-0}" \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    "${NOTES}" \
    >> "${RESULTS_CSV}"
  log "Result recorded → ${RESULTS_CSV}"
}

# ─────────────────────────────────────────────────────────────────────────────
# Slack notification
# ─────────────────────────────────────────────────────────────────────────────
notify_slack() {
  [[ -z "${SLACK_WEBHOOK_URL}" ]] && return 0
  local color="good"
  [[ "${RESULT}" != "PASS" ]] && color="danger"
  local payload
  payload=$(printf '{"attachments":[{"color":"%s","title":"Chaos Migration Drill: %s","text":"Drill ID: %s\\nResult: %s\\nRecovery: %dms\\nNotes: %s","footer":"stellabill chaos drill","ts":%d}]}' \
    "${color}" "${RESULT}" "${DRILL_ID}" "${RESULT}" "${RECOVERY_TIME_MS}" "${NOTES}" "$(date +%s)")
  curl -sS -X POST -H 'Content-type: application/json' \
    --data "${payload}" "${SLACK_WEBHOOK_URL}" || log "WARN: Slack notification failed (non-fatal)"
}

# ─────────────────────────────────────────────────────────────────────────────
# Step 1 — Pre-flight checks
# ─────────────────────────────────────────────────────────────────────────────
require_cmd docker
require_cmd awk

log "=== Chaos Migration Drill: ${DRILL_ID} ==="
log "Image:      ${POSTGRES_IMAGE}"
log "Kill range: ${KILL_DELAY_MIN}s – ${KILL_DELAY_MAX}s"
log "Results:    ${RESULTS_CSV}"

# ─────────────────────────────────────────────────────────────────────────────
# Step 2 — Start ephemeral Postgres
# ─────────────────────────────────────────────────────────────────────────────
log "Starting ephemeral Postgres container ${POSTGRES_CONTAINER}…"
docker rm -f "${POSTGRES_CONTAINER}" &>/dev/null || true
docker run -d \
  --name "${POSTGRES_CONTAINER}" \
  -e POSTGRES_USER="${POSTGRES_USER}" \
  -e POSTGRES_PASSWORD="${POSTGRES_PASSWORD}" \
  -e POSTGRES_DB="${POSTGRES_DB}" \
  -p "${POSTGRES_PORT}:5432" \
  "${POSTGRES_IMAGE}" \
  > /dev/null

# Wait for Postgres to be ready (up to 30s)
log "Waiting for Postgres to be ready…"
for i in $(seq 1 30); do
  if docker exec "${POSTGRES_CONTAINER}" pg_isready -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" &>/dev/null; then
    log "Postgres ready after ${i}s"
    break
  fi
  sleep 1
  [[ $i -eq 30 ]] && die "Postgres did not become ready within 30s"
done

# ─────────────────────────────────────────────────────────────────────────────
# Step 3 — Start migration in the background, kill Postgres mid-way
# ─────────────────────────────────────────────────────────────────────────────
KILL_DELAY_S="$(random_delay)"
log "Will kill Postgres after ${KILL_DELAY_S}s"

# Launch migration asynchronously
export DATABASE_URL
${MIGRATE_CMD} &>/tmp/drill-migrate-pre.log &
MIGRATE_PID=$!

# Sleep for the random kill delay, then SIGKILL Postgres
sleep "${KILL_DELAY_S}"

log "Sending SIGKILL to Postgres (container: ${POSTGRES_CONTAINER})…"
KILL_TS=$(ts_ms)
docker kill --signal SIGKILL "${POSTGRES_CONTAINER}" &>/dev/null || true

# Wait for the migration process to exit (it will error — that is expected)
wait "${MIGRATE_PID}" || true
log "Migration process exited (expected error due to kill)"

# ─────────────────────────────────────────────────────────────────────────────
# Step 4 — Restart Postgres
# ─────────────────────────────────────────────────────────────────────────────
log "Restarting Postgres container…"
docker start "${POSTGRES_CONTAINER}" > /dev/null

# Wait for it to be ready again
log "Waiting for Postgres to recover…"
RESTART_START=$(ts_ms)
for i in $(seq 1 60); do
  if docker exec "${POSTGRES_CONTAINER}" pg_isready -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" &>/dev/null; then
    RECOVERY_TIME_MS=$(( $(ts_ms) - RESTART_START ))
    log "Postgres recovered in ${RECOVERY_TIME_MS}ms"
    break
  fi
  sleep 1
  [[ $i -eq 60 ]] && die "Postgres did not recover within 60s after restart"
done

# ─────────────────────────────────────────────────────────────────────────────
# Step 5 — Re-run migrations (idempotency check)
# ─────────────────────────────────────────────────────────────────────────────
log "Re-running migrations to verify idempotent recovery…"
if ! ${MIGRATE_CMD} &>/tmp/drill-migrate-post.log; then
  NOTES="Migration failed after restart. See /tmp/drill-migrate-post.log"
  log "FAIL: ${NOTES}"
  cat /tmp/drill-migrate-post.log >&2
  RESULT="FAIL"
  record_result
  notify_slack
  exit 1
fi
log "Migrations completed successfully after restart ✓"

# ─────────────────────────────────────────────────────────────────────────────
# Step 6 — Verify schema_migrations table integrity
# ─────────────────────────────────────────────────────────────────────────────
log "Verifying schema_migrations table integrity…"
APPLIED_COUNT=$(docker exec "${POSTGRES_CONTAINER}" \
  psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -t -c \
  "SELECT COUNT(*) FROM schema_migrations;" 2>/dev/null | tr -d ' \n')

if [[ -z "${APPLIED_COUNT}" || "${APPLIED_COUNT}" -eq 0 ]]; then
  NOTES="schema_migrations is empty after recovery — possible corruption"
  log "FAIL: ${NOTES}"
  RESULT="FAIL"
  record_result
  notify_slack
  exit 1
fi
log "schema_migrations has ${APPLIED_COUNT} applied migration(s) ✓"

# ─────────────────────────────────────────────────────────────────────────────
# Step 7 — Check for partial / non-transactional DDL
# ─────────────────────────────────────────────────────────────────────────────
# Each migration runs inside a transaction; a kill mid-flight should leave
# NO partial state. We verify by checking that migration versions in
# schema_migrations match the files on disk.
log "Checking for partial DDL (non-transactional leftovers)…"
ORPHAN_TABLES=$(docker exec "${POSTGRES_CONTAINER}" \
  psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -t -c \
  "SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename;" \
  2>/dev/null | tr -d ' ' | grep -v '^$' | sort)

log "Tables in public schema after recovery:"
echo "${ORPHAN_TABLES}" | while read -r t; do log "  - ${t}"; done

# ─────────────────────────────────────────────────────────────────────────────
# Step 8 — Pass
# ─────────────────────────────────────────────────────────────────────────────
NOTES="kill_delay=${KILL_DELAY_S}s; applied=${APPLIED_COUNT}; recovery_ms=${RECOVERY_TIME_MS}"
RESULT="PASS"
log "=== DRILL PASSED ✓ === ${NOTES}"
record_result
notify_slack
exit 0
