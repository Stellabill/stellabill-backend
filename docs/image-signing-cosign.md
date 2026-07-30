# Image Signing with Cosign & Kyverno Enforcement

This document is the operator / on-call runbook for the image supply chain
that ships with `stellabill-backend`. It explains what is signed, by whom, and
how a Kubernetes cluster rejects unsigned or forged images at admission.

It accompanies:

- `.github/workflows/release.yml` — build, sign, attest, verify
- `deploy/policies/kyverno-verify-images.yaml` — admission policy
- `internal/deploylint/` — Go-level invariants tests that run on every CI build

---

## 1. What gets signed and where

On every release tag push (`vX.Y.Z`), the `release.yml` workflow builds a
multi-architecture image (`linux/amd64` and `linux/arm64`), pushes it to
GHCR, then performs three security operations in sequence:

| Step | Action | Output |
|---|---|---|
| 1. Build | `docker/build-push-action@v5` | Image `@sha256:…` pushed to `ghcr.io/stellabill/stellabill-backend` |
| 2. Sign | `cosign sign --yes` (keyless) | `.sig` OCI layer in GHCR (Fulcio certificate, Rekor transparency) |
| 3. Attest SLSA | `cosign attest --type slsaprovenance --predicate provenance.intoto.jsonl` | `.att` OCI layer |
| 4. Verify | `cosign verify` + `cosign verify-attestation` | Workflow refuses to declare success unless both pass |

The image is always referenced by its immutable digest (`@sha256:…`) and never
by `latest` — that way a later tag rewrite can never invalidate a signature
already admitted by Kyverno.

> **Identity & OIDC issuer**: subject =
> `https://github.com/<owner>/stellabill-backend/.github/workflows/release.yml@refs/tags/<version>`
> issuer = `https://token.actions.githubusercontent.com`.

## 2. Why the SLSA provenance predicate matters

The SLSA v1 predicate proves *how* the artifact was built. Without it,
Kyverno can only answer "was this artifact signed by the release workflow?"
but not "was this artifact built from the tag I expected?"

The provenance pipeline is intentionally minimal but auditable — see
[the in-toto statement format](https://github.com/in-toto/attestation/blob/main/protos/in_toto_attestation.proto).
We do not depend on the heavyweight `slsa-framework/slsa-github-generator`
because hand-rolling the keep-it-small predicate keeps the security model
easy to review in PRs. To upgrade to SLSA Build Level 3 we can adopt
the framework later — the policy already only requires v1.

## 3. Local verification

Before a release tag is rolled out to a cluster, an operator can re-verify
the signature from a workstation. The following commands only depend on
[`cosign`](https://docs.sigstore.dev/cosign/installation) and `curl` /
`crane`.

```bash
# Pull the manifest digest from GHCR.
DIGEST=$(crane manifest ghcr.io/stellabill/stellabill-backend:v1.2.3 \
  | crane digest)

# Verify cosign signature.
cosign verify \
  --certificate-identity-regexp 'https://github.com/.+/stellabill-backend/.github/workflows/release.yml@refs/tags/.+' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "ghcr.io/stellabill/stellabill-backend@${DIGEST}"

# Verify SLSA provenance.
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-identity-regexp 'https://github.com/.+/stellabill-backend/.github/workflows/release.yml@refs/tags/.+' \
  --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
  "ghcr.io/stellabill/stellabill-backend@${DIGEST}"
```

If any command exits non-zero, the image **must not** be promoted — see the
rollback playbook for the cluster policy below.

## 4. Cluster admission

`deploy/policies/kyverno-verify-images.yaml` is a Kyverno `ClusterPolicy`
applied to every cluster that hosts Stellabill workloads. It enforces in two
independent checks:

1. **`verifyImages.attestors`** — the digest must be signed by Fulcio via
   GitHub OIDC for the `release.yml` workflow on a tag ref.
2. **`verifyImages.attestations`** — the same identity must additionally
   sign an attestation whose predicate type is
   `https://slsa.dev/provenance/v1`.

`validationFailureAction: Enforce` means rejected images cannot land.
Switching to `Audit` is supported as a stepped migration (the same file
will validate), but should never be the steady-state in production.

## 5. Rollback playbook

If the signing service, the Kyverno policy, or any single artifact becomes
unavailable or compromised, follow the playbook below. The goal is
**safe-degradation** — never bypass a control without an audit trail.

### 5.1 Sigstore (Fulcio / Rekor) outage

Symptoms: `cosign sign` or `cosign verify` timings out, HTTP 5xx from
`https://fulcio.sigstore.dev` or `https://rekor.sigstore.dev`.

Immediate response:

1. **Do not edit the cluster policy yet.** Tagged releases already have valid
   Rekor entries — admission continues to work for previously-deployed
   digests.
2. Re-run the `sign` and `attest-slsa` jobs in the failing release workflow
   (`Actions → release.yml → Re-run failed jobs`). Cosign signing is
   idempotent: re-signing the same digest adds a new Rekor entry alongside
   existing ones, so retrying is safe.
3. If the outage exceeds 30 minutes, pause new releases and draft an
   incident postmortem; do not promote a release that lacks provenance.

Recovery: when Sigstore is back, no extra action is required — the
re-signed image overwrites the unstamped metadata and admission resumes.

### 5.2 Kyverno ClusterPolicy misconfiguration

A bad `verifyImages` rule can block previously working deployments. To
revert:

```bash
# Audit the failing pods first.
kubectl get events --all-namespaces --field-selector reason=Failed -o json \
  | jq '.items[] | {namespace, pod: .involvedObject.name, message: .message}' \
  | less

# Temporarily suspend enforcement of just this policy (keeps the rule in
# Audit mode while you fix it).
kubectl annotate clusterpolicy verify-stellabill-images \
  policies.kyverno.io/severity=low --overwrite

# Or delete the policy entirely if you need an immediate lift.
kubectl delete clusterpolicy verify-stellabill-images
```

If the policy is deleted, **every Stellabill deployment pod is at risk of
running an unsigned image** — schedule a remediation PR and re-apply as
soon as the cause is understood.

### 5.3 Emergency bypass via PolicyException

For a single workload that urgently needs an unverified image (for example
a debug pod running a tool not yet shipped from CI), prefer a `PolicyException`
over deleting the ClusterPolicy — it keeps everything else locked-down:

```yaml
apiVersion: kyverno.io/v2
kind: PolicyException
metadata:
  name: allow-debug-pod-2025-04-12
  namespace: default
spec:
  exceptions:
    - policyName: verify-stellabill-images
      ruleNames: ["require-cosign-and-slsa"]
  match:
    any:
      - resources:
          kinds: ["Pod"]
          names: ["debug-stellabill-*"]
  conditions: []
  ttl: 24h        # expires automatically
  justification: "Investigating oncall incident INC-1234; revoke after."
```

Every exception has an audit-time TTL. Do not create exceptions with `ttl: 0`.

### 5.4 Re-signing a previous image

A previous tag cannot be re-published at a different digest because the
digest is content-addressed by the artifact bits. The supported recovery for
a "suspect tag" or compromised build environment is:

1. Cut a fresh release tag (`vX.Y.Z+1` or `vX.Y.Z-rc.N`) from the desired
   clean `git ref`. The release workflow signs that digest with a *new*
   Rekor entry and a new certificate whose `invocationId` reflects the new
   run.
2. The old digest remains in GHCR and in the Rekor transparency log until
   you explicitly delete the GHCR package AND rotate the subject regex in
   the Kyverno policy if the old identity is considered untrusted. Kyverno
   admission for *new* pods continues to require signatures from the trusted
   identities only.
3. If the old digest was already admitted to running deployments, you must
   either rotate those pods to the new digests or `kubectl delete pod` them
   and let the controller schedule new pods against the new image.

### 5.5 Pre-flight checklist before patching the policy

- [ ] PR reviewed by two maintainers, including the security reviewer
- [ ] `cosign verify` locally succeeds against the latest release
- [ ] `kubectl --dry-run=server -f deploy/policies/kyverno-verify-images.yaml` returns the expected diff
- [ ] At least one staging cluster has been re-applied end-to-end and reproduced admission (success for tagged image, fail for unsigned)

## 6. Security assumptions & threat model

What this setup gives us:

- **Authenticity**: SIGN(step 2) proves the image was built by *our* GitHub
  workflow; not a developer's local docker build.
- **Integrity**: SLSA attest(step 3) ties the artifact to its source
  checkout (commit SHA, workflow file, invocation ID).
- **Confidentiality**: out of scope — the image is public on GHCR.

> **No in-image HEALTHCHECK**: the runtime stage uses
> `gcr.io/distroless/cc-debian12:nonroot` which has no shell, so we cannot
> express `HEALTHCHECK`inside the image. Instead we rely on the
> Kubernetes/kyverno readiness probe plus the cosign signature/attestation
> checks at admission to keep bad pods off the cluster.

- No protection against a compromised GitHub Actions runner. Pair with
  GitHub's ephemeral runners and the existing `permissions: {}` → `permissions: write` opt-in model.
- No protection against a malicious PR that mutates the workflow file. PR
  reviews and branch protections are the control here.
- No protection against cluster-side threats (compromised Kyverno
  controller, etc.) — see the cluster security review separately.

## 7. Troubleshooting matrix

| Symptom | Likely cause | First action |
|---|---|---|
| `cosign sign` exits with `failed to get OIDC token` | missing `id-token: write` permission | check job permissions in the workflow, run `go test ./internal/deploylint/...` |
| `cosign verify` exits with `no matching signatures` | wrong tag ref / wrong subject regex | confirm tag and update regex in policy if intentional |
| `cosign verify` exits with `transparency log offline` | Rekor outage | fall back to `--insecure-ignore-tlog` ONLY in incident response, document in postmortem |
| Kyverno admits an unsigned image | `--validationFailureAction Audit` accidentally set | re-apply `Enforce` from the policy YAML |
| Pod stuck in `Pending` after upgrade | registry outage | check GHCR status page; pod retries indefinitely |

## 8. Where this doc lives in the ADR lineage

When we move to SLSA Build Level 3 with `slsa-framework/slsa-github-generator`,
add an ADR under `docs/adr/` linking back to this file. The `internal/deploylint`
tests are the guard rails that prevent the policy file from drifting out of
sync with the documentation.
