# K6 Soak Test: Statements Endpoint

## Purpose
Validates `/api/v1/statements` endpoint under sustained 1-hour traffic load.

## SLA Thresholds
- **p95 Response Time**: < 250ms
- **p99 Response Time**: < 500ms
- **Non-2xx Requests**: 0 allowed

## Local Run (2 minutes)
```bash
k6 run tests/k6/statements_soak.js --duration=2m --vus=10
```

## Staging/Prod Run
Trigger via GitHub Actions: Actions → K6 Soak Test → Run workflow

## Output
Results saved to `results.json` as artifact.
