# Dependency Security Scanning Policy

## Overview

This document outlines the policy for managing dependency vulnerabilities and license compliance for the Stellabill backend project.

## Scanning Tools

| Tool | Purpose | Frequency |
|------|---------|-----------|
| govulncheck | Go vulnerability scanning | Every push/PR |
| OSV Scanner | General vulnerability detection | Every push/PR |
| Dependency Review | GitHub dependency review | Via GitHub Action |
| go-licenses | License compliance | Weekly |

## Severity Policy

| Severity | Definition | Remediation Timeframe |
|----------|------------|----------------------|
| Critical | Remote code execution, data breach risk | 24 hours |
| High | Privilege escalation, service disruption | 7 days |
| Medium | Information disclosure, limited impact | 30 days |
| Low | Best practice violation, documentation | 90 days |

## Remediation Process

### Step 1: Identify
Run the dependency scanning workflow to identify vulnerabilities:
```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
govulncheck ./...
```

### Step 2: Assess
Evaluate the vulnerability:
- Is there a known exploit?
- What's the severity level?
- Are there compensating controls?

### Step 3: Fix
Options in order of preference:
1. **Upgrade**: Update to a patched version
2. **Replace**: Switch to a secure alternative
3. **Mitigate**: Apply workaround (with documentation)
4. **Accept**: Document risk acceptance (requires approval)

### Step 4: Verify
Re-run scans to confirm remediation.

### Step 5: Monitor
Watch for new vulnerabilities in dependencies.

## License Compliance

### Prohibited Licenses
- GPL-3.0 (unless using for local-only execution)
- AGPL-3.0
- SSPL
- Commercial licenses that restrict use

### Allowed Licenses
- MIT
- Apache-2.0
- BSD-2/3-Clause
- ISC

## Exceptions

To request an exception:
1. Document the rationale
2. Get approval from security lead
3. Document compensating controls
4. Set review date

## Reporting

Security vulnerabilities should be reported via private GitHub issues with the `security` label.

## Review

This policy is reviewed quarterly and updated as needed.