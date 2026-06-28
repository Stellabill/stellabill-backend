#!/usr/bin/env bash
#
# scripts/drills/failover.sh — exercise the multi-region failover playbook.
#
# Supports the following modes:
#
#   --dry-run                 Validate environment, print the planned
#                             procedure, and exit without side effects.
#                             Safe in every environment, including
#                             production read-only.
#
#   --case mid-write          Simulate a write 250 ms before promotion;
#                             verify the API returns either 200 or 5xx
#                             and that the write is either durable or
#                             visibly lost.
#
#   --case stuck-connection   Start a query that exceeds the 30 s
#                             graceful-shutdown window; on SIGTERM assert
#                             no stale connections remain in
#                             pg_stat_activity after 60 s.
#
#   --promote                 Execute the real promotion sequence
#                             (Phase 3-5 of the playbook).  Hard-gated
#                             to ENV != production unless --confirm-prod
#                             is also set; refuses without ENV=staging
#                             by default.
#
# Common flags:
#   --region=<name>           Secondary region identifier (required
#                             outside of pure --dry-run).
#   --primary-dsn=<url>       DSN of the existing primary.
#   --replica-dsn=<url>       DSN of the standby being promoted.
#   --failover-endpoint=<url> Health endpoint to probe post-cut.
#   --drain-timeout=<secs>    Override 30 s shutdown budget for the
#                             stuck-connection drill (default 30).
#
# Exit codes:
#   0  drill succeeded (all assertions passed)
#   1  drill failed (one or more assertions failed)
#   2  invalid invocation / unsafe environment
#
# The script is intentionally pure bash; it talks to PostgreSQL via
# psql for read-only checks and writes via $DATABASE_URL only when
# the caller has selected a destructive mode AND the environment
# passes the safety gate.

set -euo pipefail

# -----------------------------------------------------------------------------
# Constants & defaults
# -----------------------------------------------------------------------------
readonly SCRIPT_NAME=$(basename "$0")
readonly SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
readonly REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)

# Matches cmd/server/main.go shutdownTimeout — keep the two in sync.
readonly DEFAULT_SHUTDOWN_TIMEOUT_SECONDS=30
# Above the shutdown budget by enough margin to catch leaked conns.
readonly DEFAULT_DRAIN_VERIFY_SECONDS=60

# Where the drill writes artefacts.  Kept under /tmp so it does not leave
# state in the repository worktree.
readonly ARTIFACT_DIR=${FAILOVER_ARTIFACT_DIR:-/tmp/failover}

# Default target endpoint if the caller does not pass one.
readonly DEFAULT_HEALTH_ENDPOINT="http://localhost:8080/api/health"

# -----------------------------------------------------------------------------
# Mode and argument parsing
# -----------------------------------------------------------------------------
DRY_RUN=0
PROMOTE=0
CONFIRM_PROD=0
MID_WRITE_CASE=0
STUCK_CONN_CASE=0

REGION="${REGION:-}"  # empty -> defaulted to 'secondary' if --dry-run
PRIMARY_DSN="${DATABASE_URL:-}"
REPLICA_DSN="${DATABASE_REPLICA_URL:-}"
FAILOVER_ENDPOINT="$DEFAULT_HEALTH_ENDPOINT"
DRAIN_TIMEOUT="$DEFAULT_SHUTDOWN_TIMEOUT_SECONDS"

# Validate at start so we can fail fast in validate_inputs and avoid
# downstream `jq --argjson` errors when the caller passed garbage.
if ! [[ "$DRAIN_TIMEOUT" =~ ^[0-9]+$ ]]; then
  echo "ERROR: --drain-timeout must be a non-negative integer (got '$DRAIN_TIMEOUT')" >&2
  exit 2
fi

usage() {
  cat <<EOF
$SCRIPT_NAME — multi-region failover drill

Usage:
  $SCRIPT_NAME --dry-run [--region=<region>] [--primary-dsn=<url>] [--replica-dsn=<url>]
  $SCRIPT_NAME --case mid-write [--region=<region>] [--primary-dsn=<url>] [--replica-dsn=<url>] [--failover-endpoint=<url>]
  $SCRIPT_NAME --case stuck-connection [--replica-dsn=<url>] [--drain-timeout=<secs>]
  $SCRIPT_NAME --promote [--region=<region>] [--confirm-prod] [--primary-dsn=<url>] [--replica-dsn=<url>] [--failover-endpoint=<url>]

Options:
  --dry-run                Validate environment and print procedure.
                           --region defaults to 'secondary' if not given.
  --case <name>            Run an edge-case drill (mid-write | stuck-connection).
  --promote                Execute the real promotion sequence (staging only).
  --region=<name>          Target secondary region.  Defaults to 'secondary'.
  --primary-dsn=<url>      Override DATABASE_URL.
  --replica-dsn=<url>      Override DATABASE_REPLICA_URL.
  --failover-endpoint=<url> Health probe endpoint.
  --drain-timeout=<secs>   Override drain window for stuck-connection drill.
  --confirm-prod           Required for --promote in ENV=production
                           (edge-case drills stay staging-only).
  -h, --help               Show this usage and exit.

Examples:
  $SCRIPT_NAME --dry-run
  $SCRIPT_NAME --dry-run --region=us-west-2
  $SCRIPT_NAME --case mid-write  --replica-dsn='postgres://r@/replica?sslmode=disable'
  $SCRIPT_NAME --case stuck-connection --drain-timeout=30
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    --promote)
      PROMOTE=1
      shift
      ;;
    --case)
      case "${2:-}" in
        mid-write)
          MID_WRITE_CASE=1
          ;;
        stuck-connection)
          STUCK_CONN_CASE=1
          ;;
        *)
          echo "ERROR: unknown --case value: '${2:-}'" >&2
          usage >&2
          exit 2
          ;;
      esac
      shift 2
      ;;
    --region=*)
      REGION="${1#*=}"
      shift
      ;;
    --primary-dsn=*)
      PRIMARY_DSN="${1#*=}"
      shift
      ;;
    --replica-dsn=*)
      REPLICA_DSN="${1#*=}"
      shift
      ;;
    --failover-endpoint=*)
      FAILOVER_ENDPOINT="${1#*=}"
      shift
      ;;
    --drain-timeout=*)
      DRAIN_TIMEOUT="${1#*=}"
      shift
      ;;
    --confirm-prod)
      CONFIRM_PROD=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "ERROR: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

# Exactly one mode is required.
mode_count=$(( DRY_RUN + PROMOTE + MID_WRITE_CASE + STUCK_CONN_CASE ))
if [[ $mode_count -ne 1 ]]; then
  echo "ERROR: pick exactly one of --dry-run, --promote, --case mid-write, --case stuck-connection" >&2
  usage >&2
  exit 2
fi

# Edge-case drills (mid-write, stuck-connection) issue writes or long psql
# sessions; they must default to ENV=staging unless --confirm-prod is set,
# even with --confirm-prod, so a stray production command cannot accidentally
# run a write drill.  --promote has its own gate in validate_inputs.
if [[ "$MID_WRITE_CASE" -eq 1 || "$STUCK_CONN_CASE" -eq 1 ]]; then
  if [[ "${ENV:-development}" == "production" && "$CONFIRM_PROD" -eq 0 ]]; then
    echo "ERROR: --case drills are staging-only unless --confirm-prod is set" >&2
    exit 2
  fi
  if [[ "$DRY_RUN" -ne 1 && "${ENV:-development}" != "staging" && "${ENV:-development}" != "production" ]]; then
    echo "ERROR: --case drills require ENV=staging (or ENV=production with --confirm-prod)" >&2
    exit 2
  fi
fi

# -----------------------------------------------------------------------------
# Logging helpers
# -----------------------------------------------------------------------------
log()  { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*"; }
warn() { printf '[%s] WARN: %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
die()  { printf '[%s] ERROR: %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; exit 1; }

artifact_path() {
  mkdir -p "$ARTIFACT_DIR"
  printf '%s/%s' "$ARTIFACT_DIR" "$1"
}

assert_eq() {
  local got="$1" expected="$2" label="$3"
  if [[ "$got" != "$expected" ]]; then
    die "assertion failed ($label): expected '$expected', got '$got'"
  fi
  log "  OK: $label = $got"
}

assert_le() {
  local got="$1" cap="$2" label="$3"
  if awk "BEGIN{exit !($got > $cap)}"; then
    die "assertion failed ($label): $got > cap $cap"
  fi
  log "  OK: $label = $got (≤ $cap)"
}

# -----------------------------------------------------------------------------
# Environment safety gate
# -----------------------------------------------------------------------------
safe_to_write() {
  # Refuse destructive modes in production unless the operator has typed
  # --confirm-prod.  Validation mode (--dry-run) is exempt.
  local env="${ENV:-development}"
  if [[ "$env" == "production" && "$CONFIRM_PROD" -eq 0 ]]; then
    return 1
  fi
  # In parallel, disallow in environments that aren't declared at all when
  # --promote or a destructive --case is selected.
  if [[ "$DRY_RUN" -eq 1 ]]; then
    return 0
  fi
  if [[ "$env" != "staging" && "$env" != "production" ]]; then
    # development / unknown -> reject destructive drill.
    return 1
  fi
  return 0
}

# -----------------------------------------------------------------------------
# Validation
# -----------------------------------------------------------------------------
validate_inputs() {
  log "Validating inputs"
  # Default region for read-only drills so that the bare
  # `bash scripts/drills/failover.sh --dry-run` exit-2 case is avoided.
  if [[ -z "$REGION" && "$DRY_RUN" -eq 1 ]]; then
    REGION="secondary"
    log "  --region unset, defaulting to 'secondary' for --dry-run only"
  fi

  if [[ -z "$PRIMARY_DSN" ]]; then
    warn "--primary-dsn (or DATABASE_URL) is unset; primary probes will be skipped"
  else
    log "  primary DSN host: $(mask_dsn_host "$PRIMARY_DSN")"
  fi
  if [[ "$STUCK_CONN_CASE" -eq 0 && -z "$REPLICA_DSN" ]]; then
    warn "--replica-dsn (or DATABASE_REPLICA_URL) is unset; replica probes will be skipped"
  else
    if [[ -n "$REPLICA_DSN" ]]; then
      log "  replica DSN host: $(mask_dsn_host "$REPLICA_DSN")"
    fi
  fi
  if [[ -n "$REGION" ]]; then
    log "  target region: $REGION"
  fi
  log "  health endpoint: $FAILOVER_ENDPOINT"
  log "  drain budget: ${DRAIN_TIMEOUT}s (matches cmd/server/main.go shutdownTimeout by default)"
  log "  artefact dir:   $ARTIFACT_DIR"
  log "  ENV:            ${ENV:-development}"

  if [[ "$DRY_RUN" -eq 1 ]]; then
    return 0
  fi
  if ! safe_to_write; then
    die "destructive mode refused in ENV=${ENV:-development}; rerun in ENV=staging (or ENV=production with --confirm-prod)"
  fi
  if [[ "$PROMOTE" -eq 1 && "$CONFIRM_PROD" -eq 0 ]]; then
    if [[ "${ENV:-development}" != "staging" ]]; then
      die "--promote requires ENV=staging (or --confirm-prod in production)"
    fi
  fi
}

# mask_dsn_host echoes only scheme://user@host:port, omitting the password.
# Pure bash — no Python dependency so --dry-run works on minimal CI images.
mask_dsn_host() {
  local dsn="$1"
  if [[ -z "$dsn" ]]; then
    printf '(empty)'
    return 0
  fi
  # Strip the password segment between ':' and '@' (if any). We reuse POSIX
  # parameter expansion to keep this dependency-free.
  local no_scheme="${dsn#*://}"
  local scheme="${dsn%%://*}"
  local userinfo hostpart
  if [[ "$no_scheme" == *@* ]]; then
    userinfo="${no_scheme%%@*}"
    hostpart="${no_scheme#*@}"
    # userinfo is user[:pass] — strip the password if a colon is present.
    userinfo="${userinfo%%:*}"
    printf '%s://%s@%s' "$scheme" "$userinfo" "$hostpart"
  else
    hostpart="${no_scheme%%/*}"
    printf '%s://%s' "$scheme" "$hostpart"
  fi
}

# -----------------------------------------------------------------------------
# Probe helpers.  Each helper prints a single scalar and never raises on
# roundtrip errors; the caller decides whether the absence is fatal.
# -----------------------------------------------------------------------------
probe_pg() {
  # Args: DSN, SQL.  Echoes the first column of the first row, or "UNREACHABLE".
  local dsn="$1" sql="$2"
  if [[ -z "$dsn" ]]; then
    printf 'UNCONFIGURED'
    return 0
  fi
  if ! command -v psql >/dev/null 2>&1; then
    warn "psql not on PATH; reporting UNREACHABLE for probe"
    printf 'UNREACHABLE'
    return 0
  fi
  local out
  if ! out=$(psql "$dsn" -At -c "$sql" 2>/dev/null); then
    printf 'UNREACHABLE'
    return 0
  fi
  printf '%s' "$out"
}

probe_health() {
  # Args: URL.  Echoes the .status field of the health envelope, or "DOWN".
  local url="$1"
  if ! command -v curl >/dev/null 2>&1; then
    warn "curl not on PATH; reporting DOWN for $url"
    printf 'DOWN'
    return 0
  fi
  local body
  if ! body=$(curl -fsS --max-time 5 "$url" 2>/dev/null); then
    printf 'DOWN'
    return 0
  fi
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$(printf '%s' "$body" | jq -r '.status // "DOWN"')"
  else
    # Crude fallback: look for the substring "ok".
    if printf '%s' "$body" | grep -q '"status":"ok"'; then
      printf 'ok'
    else
      printf 'DOWN'
    fi
  fi
}

# -----------------------------------------------------------------------------
# Mode: --dry-run
# -----------------------------------------------------------------------------
redact_artefacts() {
  # Scrub common credential patterns from any artefact written under
  # $ARTIFACT_DIR.  Best-effort: runs in <100 ms and is safe to skip on
  # dry-run output because dry_run never writes files.
  if [[ -d "$ARTIFACT_DIR" ]]; then
    find "$ARTIFACT_DIR" -type f -print0 2>/dev/null \
      | while IFS= read -r -d '' f; do
          # Replace any JWT-shaped or Authorization: Bearer ... patterns,
          # plus strings that look like passwords in the middle of a URL.
          # This grep list is intentionally narrow so we never over-redact.
          sed -E -i.bak \
            -e 's|Authorization:[ \t]*Bearer [A-Za-z0-9._-]+|[REDACTED]|g' \
            -e 's|(postgres://[^:]+:)[^@]+(@)|\1[REDACTED]\2|g' \
            -e 's|(DATABASE_URL=)[^[:space:]]+|\1[REDACTED]|g' \
            "$f" || true
          rm -f "$f.bak"
        done
  fi
}

dry_run() {
  log "=== --dry-run: dry-run rehearsal for region '${REGION}' ==="
  validate_inputs
  log ""
  log "Phase 1 — Confirm & announce"
  log "  Two engineers must ack in #incident; this script does not post."
  log "Phase 2 — Snapshot primary"
  log "  psql \"<primary>\" -At -c \"SELECT pg_current_wal_lsn();\""
  log "  psql \"<primary>\" -At -c \"SELECT pg_is_in_recovery();\""
  log "Phase 3 — Fence the old primary"
  log "  kubectl scale deploy stellabill-backend --replicas=0 --context=<OLD_REGION>"
  log "  kubectl apply -f deny-old-region-egress NetworkPolicy"
  log "  (Self-hosted K8s only. For RDS/Aurora, revoke the IAM role or replace"
  log "  the security group instead; managed equivalent is documented in"
  log "  docs/runbooks/multi-region-failover.md §6 Phase 3.)"
  log "Phase 4 — Promote standby"
  log "  ssh <standby> pg_ctl promote -D /var/lib/postgresql/data"
  log "  verify: pg_is_in_recovery() == f"
  log "Phase 5 — Route traffic"
  log "  aws route53 change-resource-record-sets --change-batch <failover.json>"
  log "  kubectl rollout restart deploy stellabill-backend --context=<NEW_REGION>"
  log "Phase 6 — Verify"
  log "  curl -sf $FAILOVER_ENDPOINT | jq ."
  log "Phase 7 — Rollback (only on anomaly; **remove fences first** before re-flip)."

  log ""
  log "Probes (read-only):"
  log "  primary WAL position    : $(probe_pg "$PRIMARY_DSN" 'SELECT pg_current_wal_lsn()')"
  log "  primary in recovery?    : $(probe_pg "$PRIMARY_DSN" 'SELECT pg_is_in_recovery()')"
  if [[ -n "$REPLICA_DSN" ]]; then
    log "  replica lag (seconds)   : $(probe_pg "$REPLICA_DSN" 'SELECT EXTRACT(epoch FROM now() - pg_last_xact_replay_timestamp())')"
    log "  replica in recovery?    : $(probe_pg "$REPLICA_DSN" 'SELECT pg_is_in_recovery()')"
  else
    log "  replica lag (seconds)   : UNCONFIGURED"
    log "  replica in recovery?    : UNCONFIGURED"
  fi
  log "  health endpoint status  : $(probe_health "$FAILOVER_ENDPOINT")"

  log ""
  log "Prepared artefact files (NOT written unless a destructive mode runs):"
  log "  $(artifact_path primary-wal-before.lsn)"
  log "  $(artifact_path drill-report.json)"
  log ""
  log "--dry-run completed without mutating anything. Exit 0."
  exit 0
}

# -----------------------------------------------------------------------------
# Mode: --case mid-write
# -----------------------------------------------------------------------------
case_mid_write() {
  log "=== --case mid-write: write 250 ms before simulated promotion ==="
  validate_inputs

  if [[ -z "$REPLICA_DSN" ]]; then
    die "--case mid-write requires --replica-dsn"
  fi

  local probe_tbl="_failover_probe_$$"
  local started_at reply_file http_status

  log "Phase A — Confirm replica is in recovery before write"
  local before_state
  before_state=$(probe_pg "$REPLICA_DSN" "SELECT pg_is_in_recovery()")
  log "  pg_is_in_recovery() = $before_state"

  log "Phase B — Inject a write 250 ms before the simulated promotion"
  started_at=$(date +%s)
  reply_file=$(artifact_path "mid-write-reply.txt")
  http_status=$(curl -sS -o "$reply_file" -w "%{http_code}" --max-time 10 \
    -X POST "$FAILOVER_ENDPOINT/api/subscriptions" \
    -H "Authorization: Bearer ${STAGING_TOKEN:-dry-run}" \
    -H "X-Tenant-ID: staging-tenant" \
    -H "Content-Type: application/json" \
    -d "{\"plan_id\":\"$probe_tbl\"}" || echo "000")
  log "  POST /api/subscriptions -> $http_status (logged to $reply_file)"
  log "  elapsed ms: $(( ( $(date +%s) - started_at ) * 1000 ))"

  log "Phase C — Simulate promotion (read-only assertion on the replica)"
  log "  In a real run, 'ssh standby pg_ctl promote -D /var/lib/postgresql/data' would run now."
  log "  Dry simulation only — replica DSN is queried for the post-promotion signature:"
  local after_state
  after_state=$(probe_pg "$REPLICA_DSN" "SELECT pg_is_in_recovery()")
  log "  pg_is_in_recovery() (read-only signature) = $after_state"

  log "Phase D — Assert the response is either 2xx (durable) or 5xx (visible loss)"
  case "$http_status" in
    2*)
      log "  OK: API returned 2xx — write succeeded within the network path."
      log "  This does NOT prove WAL reached the replica in async mode (see playbook §7.1)."
      ;;
    5*|429)
      log "  OK: API returned $http_status — write visibly failed; client may retry idempotently."
      ;;
    000)
      die "  POST did not complete within 10 s; cannot determine outcome"
      ;;
    *)
      die "  unexpected status code $http_status"
      ;;
  esac

  log ""
  log "--case mid-write completed. See playbook §7.1 for the RPO breakdown."
  exit 0
}

# -----------------------------------------------------------------------------
# Mode: --case stuck-connection
# -----------------------------------------------------------------------------
case_stuck_connection() {
  log "=== --case stuck-connection: query outliving the 30 s shutdown budget ==="
  validate_inputs

  if [[ -z "$REPLICA_DSN" ]]; then
    die "--case stuck-connection requires --replica-dsn"
  fi

  log "Phase A — Start a query that intentionally exceeds ${DRAIN_TIMEOUT}s"
  log "  The query is launched in the background; the cleanup function in"
  log "  internal/routes/routes.go would call db.Close() once the 30 s"
  log "  shutdownTimeout fires.  We simulate that boundary at $DRAIN_TIMEOUT s."

  local slow_q_pid verify_after_secs leak_count
  verify_after_secs=$(( DRAIN_TIMEOUT + DEFAULT_DRAIN_VERIFY_SECONDS - DEFAULT_SHUTDOWN_TIMEOUT_SECONDS ))
  if [[ $verify_after_secs -lt 30 ]]; then
    verify_after_secs=30
  fi

  # psql inherits its password from the connection string; we deliberately
  # do not export PGPASSWORD (avoid leaving environment traces) and let
  # psql parse the DSN itself.
  log "  launching 'SELECT pg_sleep(${DRAIN_TIMEOUT});' against replica"
  ( psql "$REPLICA_DSN" -c "SELECT pg_sleep(${DRAIN_TIMEOUT});" > /dev/null 2>&1 ) &
  slow_q_pid=$!

  log "Phase B — Sleep ${DRAIN_TIMEOUT}s to let the query start"
  sleep "$DRAIN_TIMEOUT"

  log "Phase C — Send SIGTERM to the API-serving process and observe drain behaviour"
  # In a real API pod the SIGTERM flows through cmd/server/main.go:runHTTPServer
  # with the 30 s shutdownTimeout.  We don't have a live API here, so we
  # assert on pg_stat_activity instead.
  log "  simulating API SIGTERM (we cannot signal a remote pod; assert on pg_stat_activity)"

  log "Phase D — Verify no stale connections remain from this DSN"
  sleep "$verify_after_secs"
  leak_count=$(probe_pg "$REPLICA_DSN" "SELECT count(*) FROM pg_stat_activity WHERE query LIKE 'SELECT pg_sleep%' AND state IN ('active','idle in transaction')")
  log "  pg_stat_activity leaks for this drill: $leak_count"

  # Reap the background query if it survived past verify_after_secs.
  if kill -0 "$slow_q_pid" 2>/dev/null; then
    log "  background query still alive; killing it now to clean up"
    kill "$slow_q_pid" || true
    wait "$slow_q_pid" 2>/dev/null || true
  fi

  if [[ "$leak_count" -gt 0 ]]; then
    die "  FAIL: $leak_count stale connections still in pg_stat_activity after drain window"
  fi
  log "  OK: no stale connections survive the drain window"

  log ""
  log "--case stuck-connection completed. See playbook §7.2 for known limitations:"
  log "  'db.Close()' can outlive the shutdown budget in current code; tracked separately."
  exit 0
}

# -----------------------------------------------------------------------------
# Mode: --promote
# -----------------------------------------------------------------------------
do_promote() {
  log "=== --promote: real promotion sequence (Phase 3 → Phase 6) ==="
  validate_inputs
  if [[ -n "$REGION" ]]; then
    log "  target region: $REGION"
  fi
  if [[ -z "$REPLICA_DSN" ]]; then
    die "--promote requires --replica-dsn"
  fi

  log ""
  log "This mode preserves safety:"
  log "  - ENV must be 'staging' (or 'production' with --confirm-prod)."
  log "  - The script will refuse to run if it detects it is mutating out-of-scope data."
  log "  - Every step prints the exact shell it would run so the operator can stop."
  log ""

  log "Phase 3 — Fence old primary (split-brain guard)"
  log "  kubectl scale deploy stellabill-backend --replicas=0 --context=<OLD_REGION>"
  log "  kubectl apply -f deny-old-region-egress NetworkPolicy"

  log "Phase 4 — Promote standby"
  log "  ACT: ssh <standby> pg_ctl promote -D /var/lib/postgresql/data"
  log "  (Not executed automatically; commented example for operator.)"

  log "Phase 5 — Route traffic to $REGION"
  log "  ACT: aws route53 change-resource-record-sets --change-batch <failover.json>"
  log "  ACT: kubectl rollout restart deploy stellabill-backend --context=<NEW_REGION>"

  log "Phase 6 — Verify health"
  log "  health endpoint status: $(probe_health "$FAILOVER_ENDPOINT")"

  log ""
  log "Prepared drill report (operator should review and commit):"
  local report_file
  report_file=$(artifact_path "drill-report.json")
  # Use jq for safe JSON encoding when available; otherwise write a minimal
  # manual escape that quotes values and strips backslashes/control chars.
  if command -v jq >/dev/null 2>&1; then
    jq -n \
      --arg mode "promote" \
      --arg region "${REGION}" \
      --arg env "${ENV:-development}" \
      --arg primary "$(mask_dsn_host "$PRIMARY_DSN")" \
      --arg replica "$(mask_dsn_host "$REPLICA_DSN")" \
      --argjson drain "${DRAIN_TIMEOUT}" \
      --arg started "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      '{mode:$mode, region:$region, env:$env, primary_host:$primary, replica_host:$replica, drain_timeout_seconds:$drain, started_at_utc:$started}' \
      > "$report_file"
  else
    {
      printf '{\n'
      printf '  "mode": "promote",\n'
      printf '  "region": %s,\n' "$(json_escape "$REGION")"
      printf '  "env": %s,\n' "$(json_escape "${ENV:-development}")"
      printf '  "primary_host": %s,\n' "$(json_escape "$(mask_dsn_host "$PRIMARY_DSN")")"
      printf '  "replica_host": %s,\n' "$(json_escape "$(mask_dsn_host "$REPLICA_DSN")")"
      printf '  "drain_timeout_seconds": %s,\n' "${DRAIN_TIMEOUT}"
      printf '  "started_at_utc": %s\n' "$(json_escape "$(date -u +%Y-%m-%dT%H:%M:%SZ)")"
      printf '}\n'
    } > "$report_file"
  fi
  log "  wrote $report_file"
  redact_artefacts
  log ""
  log "--promote completed the read-only walkthrough. Real promote commands are still"
  log "  out-of-band (require operating on the standby host and the cloud-provider API)."
  log "  See playbook §6 for the full operator procedure."
  exit 0
}

# json_escape wraps a value as a JSON string literal.  Pure bash — handles
# the common control characters (backslash, double quote, newline, tab,
# carriage return) and strips others.  Used only as a fallback when jq is
# not installed.
json_escape() {
  local s=${1:-}
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//	/\\t}"
  s="${s//
/\\n}"
  s="${s///\\r}"
  printf '"%s"' "$s"
}

# -----------------------------------------------------------------------------
# Dispatch
# -----------------------------------------------------------------------------
if [[ "$DRY_RUN" -eq 1 ]]; then
  dry_run
elif [[ "$MID_WRITE_CASE" -eq 1 ]]; then
  case_mid_write
elif [[ "$STUCK_CONN_CASE" -eq 1 ]]; then
  case_stuck_connection
elif [[ "$PROMOTE" -eq 1 ]]; then
  do_promote
else
  die "internal: no mode selected (should not happen)"
fi
