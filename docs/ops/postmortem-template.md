# Blameless postmortem template

**Service:** Stellabill Backend
**Owner:** On-call engineer → Backend team lead
**Last updated:** 2026-07-27

Copy this file into [`docs/ops/postmortems/`](./postmortems/) as `YYYY-MM-DD-short-slug.md`, complete every section, and add a row to the [postmortem index](./postmortems/README.md).

This template is **blameless**: focus on systems, processes, and gaps — not individual fault. Use it for production incidents **and** near-misses (security findings, cost overruns, failed deploys with no user impact).

---

## Metadata

| Field | Value |
|-------|-------|
| **Title** | _(short, searchable name)_ |
| **Incident ID** | _(PagerDuty / internal tracker ID)_ |
| **Date (UTC)** | _(primary incident date)_ |
| **Authors** | _(names or roles)_ |
| **Status** | Draft / Review / Final |
| **Incident type** | Outage / Degradation / Security / Cost / Data integrity / Other |
| **Severity** | _(see taxonomy below)_ |
| **Related runbook** | _(link, e.g. [db-outage-runbook](./db-outage-runbook.md))_ |
| **Detection** | Alert / Customer report / Internal observation / Audit |

### Severity taxonomy

Align severity with ops runbooks and PagerDuty event levels (`internal/integrations/pagerduty`):

| Severity | When to use | Examples |
|----------|-------------|----------|
| **Emergency** | Sustained Critical with broad customer impact | 5xx rate > 25 % (see [elevated-errors-runbook](./elevated-errors-runbook.md)) |
| **Critical** | Pager-worthy; SLA breach risk | `"db": "down"` > 2 min; 401 rate > 10 % |
| **Warning** | Elevated signals; no page | Slow queries, elevated 401 below critical threshold |
| **Info** | No production impact; process or cost learning | Drill gap, budget alert before spend cap |

For **security** incidents, use Critical when credentials may be exposed or tenant isolation is in question; use Warning for contained findings with no evidence of exploitation.

For **cost** incidents, use Critical when spend threatens budget caps or causes service throttling; use Info/Warning for anomalies caught before impact.

---

## 1. Summary

_(2–4 sentences: what happened, when it started and ended, and the customer-facing outcome.)_

---

## 2. Impact

### User and business impact

- **Who was affected:** _(tenants, regions, endpoints)_
- **Duration:** _(UTC start → end; note partial degradation windows)_
- **SLO / SLA:** _(error budget consumed, if applicable)_
- **Data:** _(loss, corruption, or exposure — state "none" explicitly if verified)_

### Non-outage impact (use when incident type is Security or Cost)

- **Security:** _(scope of exposure, rotated credentials, audit log review outcome)_
- **Cost:** _(estimated spend delta, services involved, budget vs. actual)_
- **Compliance / privacy:** _(if applicable)_

---

## 3. Timeline

All times in **UTC**. Link dashboards or log queries where helpful.

| Time (UTC) | Event |
|------------|-------|
| | Detection (alert fired or report received) |
| | Triage started |
| | Mitigation applied |
| | Recovery verified |
| | Incident closed |

---

## 4. Root cause

### What broke

_(Technical description: failing component, config, dependency, or process.)_

### Contributing factors

_(Why safeguards did not prevent impact: missing alert, untested path, runbook gap, capacity, human process.)_

### What went well

_(Detection speed, rollback, communication, runbook steps that helped.)_

---

## 5. Action items

Each item needs an owner, priority, and target date. Track to completion in the incident tracker.

| ID | Action | Type | Owner | Priority | Due |
|----|--------|------|-------|----------|-----|
| 1 | | Prevent / Detect / Mitigate / Process | | P0–P2 | |
| 2 | | | | | |

**Type guide:**

- **Prevent** — fix root cause (code, config, capacity)
- **Detect** — alerts, SLOs, synthetic checks
- **Mitigate** — runbook, rollback, feature flags
- **Process** — drills, documentation, training

---

## 6. Lessons learned (optional)

_(Patterns to reuse or avoid; link follow-up docs or ADRs.)_

---

## Filing checklist

- [ ] No secrets, tokens, or raw PII in this document (see [security reminders](./README.md#security-reminders))
- [ ] Row added to [postmortem index](./postmortems/README.md)
- [ ] Related runbook post-incident checklist completed
- [ ] Action items filed in tracker with owners
