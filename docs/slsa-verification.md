# SLSA Provenance Verification Guide

This document explains how to verify the SLSA Level 3 provenance produced by
the `release-go.yml` workflow for every `stellabill-backend` release.

Provenance proves that a given binary or container image was built by the
pinned GitHub Actions workflow from the tagged source commit — and not by any
other actor.

---

## Contents

- [What is generated](#what-is-generated)
- [Install slsa-verifier](#install-slsa-verifier)
- [Verify the Go binary](#verify-the-go-binary)
- [Verify the container image](#verify-the-container-image)
- [Inspect the provenance document](#inspect-the-provenance-document)
- [Automate verification in CI](#automate-verification-in-ci)
- [Error reference](#error-reference)

---

## What is generated

For every GitHub Release (`vX.Y.Z`):

| Artifact | Where |
|---|---|
| `stellabill-backend` (linux/amd64 binary) | GitHub Release assets |
| `stellabill-backend.intoto.jsonl` | GitHub Release assets — SLSA binary provenance |
| Container image `ghcr.io/Stellabill/stellabill-backend:vX.Y.Z` | GitHub Container Registry |
| Container SLSA attestation | Pushed as OCI referrer to GHCR alongside the image |

The provenance is signed using **Sigstore keyless signing** (OIDC → Fulcio CA
→ Rekor transparency log).  No pre-shared key is needed to verify it.

---

## Install slsa-verifier

```bash
# Linux/amd64 — replace VERSION with the latest release
VERSION=2.6.0
curl -sSfL \
  "https://github.com/slsa-framework/slsa-verifier/releases/download/v${VERSION}/slsa-verifier-linux-amd64" \
  -o /usr/local/bin/slsa-verifier
chmod +x /usr/local/bin/slsa-verifier
slsa-verifier version
```

Other platforms: see the [slsa-verifier releases page](https://github.com/slsa-framework/slsa-verifier/releases).

---

## Verify the Go binary

### 1. Download the binary and its provenance from the release

```bash
TAG=v1.2.3   # replace with the actual release tag

gh release download "${TAG}" \
  --repo Stellabill/stellabill-backend \
  --pattern "stellabill-backend" \
  --pattern "stellabill-backend.intoto.jsonl"
```

Or download directly:

```bash
BASE="https://github.com/Stellabill/stellabill-backend/releases/download/${TAG}"
curl -sSfLO "${BASE}/stellabill-backend"
curl -sSfLO "${BASE}/stellabill-backend.intoto.jsonl"
```

### 2. Run slsa-verifier

```bash
slsa-verifier verify-artifact stellabill-backend \
  --provenance-path stellabill-backend.intoto.jsonl \
  --source-uri     github.com/Stellabill/stellabill-backend \
  --source-tag     "${TAG}"
```

Expected output:

```
Verified build using builder https://github.com/slsa-framework/slsa-github-generator/.github/workflows/builder_go_slsa3.yml@refs/tags/v2.1.0 at commit <SHA>
PASSED: SLSA verification passed
```

Any other exit code means verification **failed** — do not use the binary.

---

## Verify the container image

### 1. Pull the image digest

```bash
TAG=v1.2.3
IMAGE=ghcr.io/Stellabill/stellabill-backend

# Pull by digest for tamper-evident verification
DIGEST=$(docker manifest inspect "${IMAGE}:${TAG}" \
  --verbose 2>/dev/null | \
  python3 -c "import sys,json; d=json.load(sys.stdin); print(d['Descriptor']['digest'])")

echo "Digest: ${DIGEST}"
```

Or retrieve the digest from the workflow output / release notes directly.

### 2. Run slsa-verifier

```bash
slsa-verifier verify-image "${IMAGE}@${DIGEST}" \
  --source-uri github.com/Stellabill/stellabill-backend \
  --source-tag "${TAG}"
```

Expected output:

```
Verified build using builder https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/v2.1.0 at commit <SHA>
PASSED: SLSA verification passed
```

### 3. Alternative: verify with cosign

```bash
# Install cosign: https://docs.sigstore.dev/cosign/installation
cosign verify-attestation \
  --type slsaprovenance \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp \
    '^https://github.com/slsa-framework/slsa-github-generator/.github/workflows/generator_container_slsa3.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+$' \
  "${IMAGE}@${DIGEST}" \
  | python3 -c "
import sys, json, base64
for line in sys.stdin:
    att = json.loads(line)
    payload = base64.b64decode(att['payload'])
    print(json.dumps(json.loads(payload), indent=2))
"
```

---

## Inspect the provenance document

To read the raw provenance DSSE envelope:

```bash
# Binary provenance
cat stellabill-backend.intoto.jsonl \
  | python3 -c "
import sys, json, base64
for line in sys.stdin:
    doc = json.loads(line)
    payload = base64.b64decode(doc['dsseEnvelope']['payload'])
    print(json.dumps(json.loads(payload), indent=2))
"
```

Key fields to inspect:

| Field | Expected value |
|---|---|
| `predicate.builder.id` | `https://github.com/slsa-framework/slsa-github-generator/.github/workflows/builder_go_slsa3.yml@refs/tags/v2.1.0` |
| `predicate.invocation.configSource.uri` | `git+https://github.com/Stellabill/stellabill-backend@refs/tags/vX.Y.Z` |
| `predicate.invocation.configSource.entryPoint` | `.github/workflows/release-go.yml` |
| `subject[0].name` | `stellabill-backend` |
| `subject[0].digest.sha256` | Must match `sha256sum stellabill-backend` |

---

## Automate verification in CI

Add this to any downstream CI that consumes the binary or image:

```yaml
- name: Install slsa-verifier
  run: |
    curl -sSfL \
      "https://github.com/slsa-framework/slsa-verifier/releases/download/v2.6.0/slsa-verifier-linux-amd64" \
      -o /usr/local/bin/slsa-verifier
    chmod +x /usr/local/bin/slsa-verifier

- name: Verify binary provenance
  env:
    TAG: v1.2.3  # pin to a specific release
  run: |
    slsa-verifier verify-artifact stellabill-backend \
      --provenance-path stellabill-backend.intoto.jsonl \
      --source-uri     github.com/Stellabill/stellabill-backend \
      --source-tag     "${TAG}"
```

---

## Staging deploy gate

The `verify-and-deploy-staging` job in `release-go.yml` already enforces this:

1. Downloads the binary and provenance artifacts produced by `binary-provenance`.
2. Calls `slsa-verifier verify-artifact` — workflow **fails** if verification fails.
3. Only on success does it proceed to the (dry-run) staging deploy step.

To convert the dry-run to a real deploy: replace the `echo` commands in the
`Staging deploy (dry-run)` step with your actual `kubectl`, `helm`, or
deployment CLI commands.

---

## Error reference

| Error message | Likely cause |
|---|---|
| `FAILED: expected source repository … but got …` | Binary was not built from the declared repo |
| `FAILED: expected source tag … but got …` | Tag mismatch — possible tampering or wrong tag supplied |
| `FAILED: builder ID does not match` | Provenance was generated by a different workflow |
| `failed to verify artifact signature` | Provenance signature invalid; possible tampering |
| `no matching attestations` | Container image has no attached provenance (container-provenance job did not run) |
| `404` on download | Provenance was not uploaded (check that `upload-assets: true` and the release is not a draft) |

Missing provenance is treated as a **hard failure** — the staging deploy job
will not run if `binary-provenance` or `container-provenance` did not succeed.
