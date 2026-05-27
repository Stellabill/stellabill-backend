# Dependency Security Scanning and Remediation Policy

## Overview

This document outlines the security scanning workflow and remediation process for dependencies in the Stellabill Backend project.

## Scanning Tools

| Tool | Purpose | Frequency |
|------|---------|-----------|
| govulncheck | Go vulnerability scanning | Weekly (schedule) + manual trigger |
| Trivy | Container/filesystem vulnerability scanning | Weekly |
| go-audit | Go dependency audit | Weekly |
| license-checker | License compliance | On push to main |

## Severity Levels

- **Critical**: RCE, remote code execution vulnerabilities
- **High**: Privilege escalation, data exfiltration
- **Medium**: DoS, information disclosure
- **Low**: Minor security concerns

## Remediation Policy

### Critical Vulnerabilities

1. **Response time**: 24 hours
2. **Action**: Upgrade to patched version or find alternative
3. **Escalation**: Notify security team immediately

### High Vulnerabilities

1. **Response time**: 7 days
2. **Action**: Plan upgrade in next sprint
3. **Workaround**: Document temporary mitigations

### Medium Vulnerabilities

1. **Response time**: 30 days
2. **Action**: Schedule upgrade in backlog

### Low Vulnerabilities

1. **Response time**: Next routine update
2. **Action**: Track and address during regular maintenance

## License Policy

### Prohibited Licenses

- GPL-3.0
- GPL-2.0
- AGPL
- NGPL

### Allowed Licenses

- MIT
- BSD (2-clause, 3-clause)
- Apache 2.0
- ISC
- MPL 2.0

## Exceptions Process

To request an exception:

1. Create issue in security repository
2. Document why the vulnerability/dependency is necessary
3. Propose compensating controls
4. Get approval from security team
5. Set review date (max 90 days)

## Reporting

- Weekly reports are generated and stored as artifacts
- Critical findings trigger immediate notifications
- Dashboard available at: https://github.com/Stellabill/stellabill-backend/security

## Update Process

1. Review scanner output weekly
2. Prioritize by severity
3. Test upgrades in staging
4. Deploy with normal release process