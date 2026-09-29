# Branch Protection & Release Gate

This document explains the branch protection rules and repository settings
required to maintain the **SLSA Level 3** guarantee produced by the
`release-go.yml` workflow.

SLSA Level 3 requires that the build process is non-forgeable and that the
provenance correctly identifies the build platform and source.  GitHub Actions
reusable workflows from `slsa-github-generator` provide this guarantee **only
when** untrusted actors cannot trigger the release workflow or tamper with the
tag it runs on.

---

## 1. Protect the `main` branch

In **Settings → Branches → Add branch protection rule** for `main`:

| Setting | Value |
|---|---|
| Require a pull request before merging | ✅ |
| Required approvals | ≥ 1 |
| Dismiss stale reviews when new commits are pushed | ✅ |
| Require status checks to pass before merging | ✅ (select `CI / Test`, `CI / Coverage`) |
| Require branches to be up to date before merging | ✅ |
| Restrict who can push to matching branches | ✅ — maintainers team only |
| Do not allow bypassing the above settings | ✅ |

> **Why**: prevents an attacker who gains write access via a stolen PAT from
> landing malicious code directly on `main` and triggering a release.

---

## 2. Restrict tag creation

In **Settings → Tags → Add tag protection rule**:

| Pattern | Who can create |
|---|---|
| `v*` | Maintainers team only |

Or, using the newer **Rulesets** UI (recommended):

1. Settings → Rules → Rulesets → New ruleset → **Tag ruleset**
2. Name: `release-tags`
3. Enforcement: **Active**
4. Target tags: `v*`
5. Rules: **Restrict creations**, **Restrict deletions**, **Restrict updates**
6. Bypass list: add the `maintainers` team (or specific trusted actors)

> **Why**: `release-go.yml` is triggered by `release: published`.  A GitHub
> Release is backed by a tag.  If anyone can create a `v*` tag, they can
> create a release and trigger the workflow from arbitrary commit SHA.

---

## 3. Require the `staging` environment approval

The `verify-and-deploy-staging` job uses the `staging` environment, which
**must** be configured with required reviewers.

In **Settings → Environments → staging**:

| Setting | Value |
|---|---|
| Required reviewers | ≥ 1 maintainer |
| Prevent self-review | ✅ |
| Wait timer | 0 min (or a short delay for emergency stops) |
| Deployment branches and tags | Selected branches/tags → `v*` tags only |

> **Why**: even with valid provenance, you may want a human approval step
> before staging.  This also gives you an emergency stop.

---

## 4. Restrict `GITHUB_TOKEN` permissions

In **Settings → Actions → General → Workflow permissions**:

- Select **Read repository contents and packages permissions** (read-only default).

Each job in `release-go.yml` declares its own `permissions:` block and requests
only what it needs (principle of least privilege).  The repo-level default
ensures no accidental escalation from unlisted jobs.

---

## 5. Allow only trusted reusable workflows (optional, advanced)

GitHub allows you to restrict which reusable workflows can be called:

In **Settings → Actions → General → Allow only specific external actions and
reusable workflows**, add:

```
slsa-framework/slsa-github-generator/.github/workflows/builder_go_slsa3.yml@*
slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@*
```

> **Note**: this requires pinning the callee to a tag (`@v2.1.0`) rather than a
> branch, which `release-go.yml` already does.

---

## 6. Summary checklist

- [ ] `main` branch protected (PR required, status checks required)
- [ ] Tag creation restricted to maintainers (`v*` pattern)
- [ ] `staging` environment requires approval from maintainers
- [ ] Repo-level `GITHUB_TOKEN` default is read-only
- [ ] (Optional) Allowed reusable workflows allowlist configured

---

## Verification after setup

To confirm the protections are active:

```bash
# Attempt to push a tag directly (should fail for non-maintainers)
git tag v0.0.0-test && git push origin v0.0.0-test
# Expected: remote: error: GH006: Protected tag

# Confirm the workflow only runs on published releases, not branch pushes
# (check the 'on:' trigger in .github/workflows/release-go.yml)
```
