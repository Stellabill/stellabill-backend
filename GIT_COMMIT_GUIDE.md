# Git Commit Guide - Health Check Implementation

## Quick Commit (Recommended)

```bash
# 1. Create and checkout feature branch
git checkout -b feature/health-dependency-checks

# 2. Add all changes
git add -A

# 3. Commit with comprehensive message
git commit -m "feat: harden health checks with dependency probes and degraded mode

Add three-tiered health check system for safer Kubernetes deployments:

- Liveness probe (/health/live): Always returns 200 if app running
- Readiness probe (/health/ready): Returns 503 if dependencies degraded  
- Health details (/health): Full dependency status for monitoring

Health checks include:
- Database connectivity with exponential backoff and timeouts
- Outbox/queue health with statistics
- Concurrent dependency checks with context timeout
- Security: no credentials or sensitive data in responses
- Comprehensive error handling and status derivation

Dependencies:
- Database: 3s timeout per ping, 2 retries with backoff
- Queue: 3s timeout, includes pending message count
- Overall: 10s timeout for readiness probe

Enables:
- Kubernetes liveness/readiness probe integration
- Safe rolling deployments without cascading failures
- Monitoring system integration (Datadog, New Relic, Prometheus)
- Degraded operation signaling for graceful degradation

Testing:
- 16 comprehensive test cases
- Database health checks (timeout, down, not configured)
- Outbox queue checks with statistics
- Status derivation (mixed healthy/degraded states)
- Concurrency and timeout handling
- Security validation (no secrets in responses)
- ~3-5s suite execution time

Documentation:
- docs/HEALTH_CHECKS.md: Complete operations guide with runbooks
- docs/HEALTH_INTEGRATION_EXAMPLE.md: Code integration patterns
- TEST_EXECUTION_HEALTH.md: Test execution and troubleshooting
- test-health.sh/test-health.bat: Automated test scripts

Files changed:
- internal/handlers/health.go: New comprehensive health check implementation
- internal/handlers/health_test.go: 16 test cases with 85%+ coverage
- internal/handlers/handler.go: Added Database/Outbox dependencies
- docs/HEALTH_CHECKS.md: Operations guide with K8s examples
- docs/HEALTH_INTEGRATION_EXAMPLE.md: Integration patterns
- TEST_EXECUTION_HEALTH.md: Test guide
- HEALTH_IMPLEMENTATION_SUMMARY.md: Feature summary
- test-health.sh: Bash test runner
- test-health.bat: Windows test runner

Fixes: #ISSUE_NUMBER (if applicable)
"

# 4. Verify commit
git log --oneline -1

# 5. Push to remote (create PR)
git push origin feature/health-dependency-checks
```

---

## Step-by-Step Commit

If you prefer to see what's being committed:

```bash
# 1. Create feature branch
git checkout -b feature/health-dependency-checks

# 2. Review what changed
git status
git diff internal/handlers/health.go | head -100  # See first 100 lines

# 3. Stage files individually (optional)
git add internal/handlers/health.go
git add internal/handlers/health_test.go
git add internal/handlers/handler.go
git add docs/HEALTH_CHECKS.md
git add docs/HEALTH_INTEGRATION_EXAMPLE.md
git add TEST_EXECUTION_HEALTH.md
git add HEALTH_IMPLEMENTATION_SUMMARY.md
git add test-health.sh
git add test-health.bat

# 4. Review staged changes
git diff --cached --stat

# 5. Commit
git commit -m "feat: harden health checks with dependency probes and degraded mode"
```

---

## Running Tests Before Commit

**IMPORTANT**: Run tests before committing to ensure everything works:

```bash
# 1. Install Go (if not already installed)
./scripts/install_go_and_run_tests.ps1  # Windows
# or
./scripts/install_go_and_run_tests.sh   # Linux/Mac

# 2. Run health check tests
sh test-health.sh  # Linux/Mac
test-health.bat    # Windows

# Expected output:
# ✓ All 16 tests passed
# Coverage: 85%+
# No race detector warnings

# 3. Build to verify compilation
go build ./cmd/server

# 4. Run full test suite
go test ./... -v
```

---

## Commit Message Breakdown

The commit message follows **Conventional Commits** format:

```
feat: <title>
<blank line>
<body>
<blank line>
<footer>
```

### Title
- Scope: `health-checks`
- Type: `feat` (new feature)
- Description: Concise summary of what this adds

### Body
- What: Three-tiered health probes (liveness, readiness, details)
- Why: Enable Kubernetes integration and safe deployments
- How: Concurrent dependency checks with timeouts
- Technical details: Specific timeouts and retry logic

### Footer
- Related issues: `Fixes: #123` if applicable
- Breaking changes: None in this case

---

## Special Commits (If Needed)

### If tests fail and you need to fix something:

```bash
# Make the fix
git add <fixed-file>

# Amend the commit (keeps same commit message)
git commit --amend --no-edit

# Force push to remote (only on your feature branch!)
git push origin feature/health-dependency-checks --force-with-lease
```

### If you need to split into multiple commits:

```bash
# Commit core implementation
git commit -m "feat: add health check types and probes

- HealthResponse struct
- HealthChecker type
- LivenessProbe, ReadinessProbe handlers
- Concurrent dependency checking"

# Commit tests
git commit -m "test: add comprehensive health check test suite

- 16 test cases covering all probe types
- Mock DBPinger and OutboxHealther
- Status derivation tests
- Security and concurrency tests"

# Commit documentation
git commit -m "docs: add health check operations guide

- Complete health checks documentation
- Kubernetes integration examples
- Test execution guide
- Operations runbooks"
```

---

## PR/MR Description Template

When creating a pull request, use this description:

```markdown
## Description
Implements comprehensive health reporting system with Kubernetes liveness/readiness probe support and dependency health tracking.

## Motivation
- Enable safe Kubernetes rolling deployments
- Integrate with monitoring systems (Datadog, New Relic, Prometheus)
- Provide visibility into dependency health (DB, queue)
- Signal degraded operation for graceful degradation

## Changes
- Add three-tiered health probes (liveness, readiness, details)
- Database health check with exponential backoff
- Outbox/queue health with statistics
- Concurrent dependency checks with timeouts
- Security: no sensitive data in responses
- 16 comprehensive test cases

## Testing
```bash
sh test-health.sh  # All 16 tests pass
go test ./... -v   # Full test suite passes
go test -race ./internal/handlers  # No race conditions
```

## Security Review
- ✅ No credentials in responses
- ✅ No stack traces or error details exposed
- ✅ Generic error messages (production-safe)
- ✅ No PII or sensitive data leakage

## Documentation
- [HEALTH_CHECKS.md](docs/HEALTH_CHECKS.md) - Complete operations guide
- [HEALTH_INTEGRATION_EXAMPLE.md](docs/HEALTH_INTEGRATION_EXAMPLE.md) - Integration examples
- [TEST_EXECUTION_HEALTH.md](TEST_EXECUTION_HEALTH.md) - Test guide

## Deployment Notes
1. Update main.go to provide DB and Outbox to Handler
2. Configure Kubernetes probes (examples in docs)
3. Monitor health endpoints during rollout
4. Adjust timeouts if needed based on real latency

## Related Issues
Closes #ISSUE_NUMBER
```

---

## After Commit - Creating a Pull Request

### GitHub
```bash
# Push your branch
git push origin feature/health-dependency-checks

# Create PR at: https://github.com/YOUR_REPO/pulls
# Select: feature/health-dependency-checks → main
```

### GitLab
```bash
# Push your branch
git push origin feature/health-dependency-checks

# Create MR at: https://gitlab.com/YOUR_REPO/-/merge_requests/new
# Select: feature/health-dependency-checks → main
```

---

## Verification Before Merge

Ensure these checks pass before merging:

```bash
# 1. Tests pass
go test ./... -v

# 2. No race conditions
go test -race ./...

# 3. Code compiles
go build ./cmd/server

# 4. Coverage is adequate
go test ./internal/handlers -cover | grep health.go
# Expected: ~85%+ coverage

# 5. Security test passes
go test ./internal/handlers -v -run TestSecurityNoSensitiveData

# 6. Lint checks (if using)
golangci-lint run ./internal/handlers/
```

---

## Merge Strategy

### Recommended: Create Merge Commit
```bash
# If using command line for merge (instead of GitHub/GitLab UI)
git checkout main
git pull origin main
git merge --no-ff feature/health-dependency-checks
git push origin main
```

### Delete Feature Branch
```bash
# Local
git branch -d feature/health-dependency-checks

# Remote
git push origin --delete feature/health-dependency-checks
```

---

## Post-Merge Tasks

1. **Deploy to Staging**
   ```bash
   # Deploy your branch to staging environment
   # Verify health endpoints work: curl /health/ready
   ```

2. **Configure Kubernetes Probes**
   - Update deployment.yaml with health check configuration
   - See docs/HEALTH_INTEGRATION_EXAMPLE.md for examples

3. **Monitor Metrics**
   ```bash
   # Watch health check metrics
   watch 'curl -s http://localhost:8080/health | jq .'
   ```

4. **Update Runbooks**
   - Link to HEALTH_CHECKS.md in ops runbooks
   - Brief team on new probes and degraded signaling

5. **Plan Next Steps**
   - Add custom health checks for app-specific dependencies
   - Export Prometheus metrics if needed
   - Set up alerting on health endpoints

---

## Rollback (If Needed)

If you need to rollback after merge:

```bash
# Option 1: Revert commit
git revert <commit-hash>
git push origin main

# Option 2: Reset to previous state
git reset --hard <commit-before-health-checks>
git push origin main --force-with-lease
```

---

## Questions?

Refer to:
- [HEALTH_IMPLEMENTATION_SUMMARY.md](HEALTH_IMPLEMENTATION_SUMMARY.md) - Feature overview
- [docs/HEALTH_CHECKS.md](docs/HEALTH_CHECKS.md) - Operations guide
- [TEST_EXECUTION_HEALTH.md](TEST_EXECUTION_HEALTH.md) - Test troubleshooting
