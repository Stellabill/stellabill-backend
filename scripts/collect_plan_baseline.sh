#!/usr/bin/env bash
set -euo pipefail

DATABASE_URL="${DATABASE_URL:-}"
if [[ -z "${DATABASE_URL}" ]]; then
  echo "DATABASE_URL must be set" >&2
  exit 2
fi

psql "$DATABASE_URL" -v ON_ERROR_STOP=1 <<'SQL'
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

INSERT INTO plan_baselines (
  query_text,
  mean_time,
  total_time,
  calls,
  shared_blks_read,
  shared_blks_hit,
  shared_blks_dirtied,
  scan_type,
  metadata
)
SELECT
  query,
  mean_exec_time,
  total_exec_time,
  calls,
  shared_blks_read,
  shared_blks_hit,
  shared_blks_dirtied,
  '',
  jsonb_build_object(
    'queryid', queryid,
    'plans', plans
  )
FROM pg_stat_statements
ORDER BY mean_exec_time DESC
LIMIT 200;
SQL
