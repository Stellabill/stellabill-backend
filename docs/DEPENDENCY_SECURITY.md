# Dependency Security Remediation Policy

## Overview

This document outlines the policy for handling dependency vulnerabilities and license compliance issues in the Stellabill backend project.

## Severity Levels

### Critical (CVSS 9.0-10.0)
- **Response Time**: 24 hours
- **Action**: Immediate mitigation required
- **Options**:
  - Upgrade to secure version
  - Replace vulnerable dependency
  - Apply vendor patch
  - Remove functionality if no fix available

### High (CVSS 7.0-8.9)
- **Response Time**: 72 hours (3 days)
- **Action**: Priority fix required
- **Options**:
  - Upgrade to secure version
  - Monitor for available fix
  - Implement workaround

### Medium (CVSS 4.0-6.9)
- **Response Time**: 2 weeks
- **Action**: Schedule fix
- **Options**:
  - Upgrade to stable version
  - Add to technical debt backlog
  - Accept risk with documentation

### Low (CVSS 0.1-3.9)
- **Response Time**: Next release cycle
- **Action**: Track and address
- **Options**:
  - Upgrade with next update
  - Monitor

## License Policy

### Allowed Licenses
- Apache-2.0
- BSD-2-Clause
- BSD-3-Clause
- ISC
- MIT
- MPL-2.0
- Go standard library

### Prohibited Licenses
- GPL-2.0 (except with linking exception)
- GPL-3.0
- AGPL-3.0
- LGPL-2.1 (direct linking)
- Any "or later" versions requiring source disclosure

### Review Process
1. New dependencies require license review before PR merge
2. Document license in code comments
3. Annual audit of all transitive dependencies

## Workflow

### On Vulnerability Detection
1. Automated alert via GitHub Security
2. Triage by security team member
3. Determine severity and assign timeline
4. Fix via version upgrade or replacement
5. Verify fix with tests
6. Document in security notes

### Exception Process
1. Create issue documenting vulnerability
2. Provide business justification
3. Document mitigation measures
4. Security team approval required
5. Set timeline for mandatory review

## Testing

All dependency updates must pass:
- `go test ./...`
- `go vet ./...`
- Integration tests
- Security scanning

## Contact

Security issues: security@stellabill.com
License questions: legal@stellabill.com