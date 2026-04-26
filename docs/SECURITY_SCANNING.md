# Dependency Security Scanning and Remediation Policy

## Overview

This document outlines the security scanning workflow and remediation process for dependencies in the Stellabill backend project. The goal is to maintain a secure codebase by identifying and addressing vulnerabilities and license compliance issues promptly.

## Scanning Workflow

### Automated Scanning

The project uses GitHub Actions to perform automated security scanning:

1. **Vulnerability Scanning** (Weekly + on `go.mod` changes)
   - Uses `govulncheck` to detect known Go vulnerabilities
   - Trivy for comprehensive container and dependency scanning

2. **License Compliance** (On every push to main)
   - Checks for prohibited licenses
   - Validates that dependencies use allowed licenses

### Scanning Schedule

| Scan Type | Frequency | Trigger |
|----------|----------|---------|
| Vulnerability Scan | Weekly | `schedule: '0 0 * * 0'` |
| Dependency Scan | On push | `go.mod`, `go.sum` changes |
| License Check | On push | `main` branch |

## Severity Classification

### Vulnerability Severity Levels

| Severity | Description | Response Time |
|----------|------------|-------------|
| CRITICAL | Remote code execution, data breach | 24 hours |
| HIGH | Privilege escalation, denial of service | 72 hours (3 days) |
| MEDIUM | Information disclosure | 7 days |
| LOW | Minimal impact | Next release cycle |

### License Categories

| Category | Status | Examples |
|----------|--------|---------|
| Allowed | ✅ Safe to use | MIT, Apache-2.0, BSD-3-Clause, ISC |
| Restricted | ⚠️ Review required | MPL-2.0, CPL-1.0 |
| Prohibited | ❌ Not allowed | GPL-2.0, GPL-3.0, AGPL-3.0, SSPL-1.0 |

## Remediation Process

### Step 1: Detection

When a scan identifies an issue:
1. A GitHub issue is auto-created with details
2. The security team is notified via GitHub alerts
3. Results are available in the workflow artifacts

### Step 2: Assessment

For each identified vulnerability:

1. **Verify the issue** - Confirm it's not a false positive
2. **Assess impact** - Determine affected components
3. **Check for mitigations** - Are there config/workaround options?
4. **Plan fix** - Update, replace, or accept risk

### Step 3: Fix Options

#### Option A: Update Dependency
```bash
go get -u github.com/example/package@latest
go mod tidy
```

#### Option B: Replace with Alternative
```bash
go get github.com/safe-alternative@latest
```

#### Option C: Accept Risk (Temporary)
- Document in `SECURITY.md` with justification
- Set timeline for resolution
- Requires security team approval

### Step 4: Verification

After implementing the fix:
1. Re-run security scans
2. Verify no new issues introduced
3. Run full test suite
4. Update vulnerability documentation

## Exception Process

Exceptions may be granted for:

1. **No Fix Available** - Vulnerability has no patch
2. **Breaking Change** - Update would introduce breaking changes
3. **Business Need** - Critical dependency with no alternatives

### Exception Request Format

```markdown
## Exception Request: [Vulnerability ID]

**Vulnerability:** [Name and CVE if applicable]
**Severity:** [CRITICAL/HIGH/MEDIUM/LOW]
**Package:** [affected package name]
**Current Version:** [version]
**Latest Version:** [latest available]
**Reason for Exception:**
[Explain why update is not feasible]

**Mitigation:**
[Describe any workarounds or guards]

**Review Date:** [Date to re-evaluate]
**Approved By:** [Security team member]
```

### Approval Authority

| Severity | Approver |
|----------|----------|
| CRITICAL | Security Lead + Engineering Lead |
| HIGH | Engineering Lead |
| MEDIUM | Senior Developer |
| LOW | Team consensus |

## Documentation Requirements

### For New Dependencies

Before adding a new dependency:

1. **License Check** - Ensure compatible license
2. **Security History** - Review past vulnerabilities
3. **Maintenance Status** - Active development?
4. **Dependents** - How many packages depend on it?

### Security Log

Maintain a `SECURITYLOG.md` in docs:

```markdown
## 2024-01-15

### Fixed
- CVE-2024-xxx in package@v1.2.3 -> v1.2.4

### Exception Granted
- golang.org/x/net@v0.x.x - No fix available, mitigation in place
- Review: 2024-02-15
```

## Contacts

### Security Team
- Primary: security@stellarbill.example.com
- On-call: [Link to rotation]

### Emergency Response
- Critical vulnerabilities: PagerDuty trigger
- Business hours: Slack #security-alerts

## Policy Review

This policy is reviewed:
- Quarterly
- After any security incident
- When new threat categories emerge

Last reviewed: [Current Date]
Next review: [Date + 3 months]