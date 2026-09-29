# plan-diff

This small tool compares a saved baseline snapshot against the latest
pg_stat_statements data and reports regressions.

## Usage

1. Capture a baseline snapshot into the database with scripts/collect_plan_baseline.sh.
2. Run the tool to compare the latest snapshot to the most recent baseline.

Example:

```bash
DATABASE_URL=postgres://... ./scripts/collect_plan_baseline.sh
go run ./tools/plan-diff
```
