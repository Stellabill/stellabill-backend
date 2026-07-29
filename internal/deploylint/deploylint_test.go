package deploylint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ─── LoadFile ─────────────────────────────────────────────────────────────────

func TestLoadFile(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		if _, err := LoadFile(""); err == nil {
			t.Fatal("expected error for empty path")
		}
	})
	t.Run("missing file", func(t *testing.T) {
		if _, err := LoadFile("/does/not/exist/please"); err == nil {
			t.Fatal("expected error for missing file")
		}
	})
	t.Run("existing file", func(t *testing.T) {
		dir := t.TempDir()
		p := filepath.Join(dir, "a.yaml")
		if err := os.WriteFile(p, []byte("k: v"), 0o600); err != nil {
			t.Fatalf("seed: %v", err)
		}
		data, err := LoadFile(p)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(data) != "k: v" {
			t.Fatalf("unexpected payload: %q", data)
		}
	})
}

// ─── ValidateKyvernoPolicy ─────────────────────────────────────────────────────

// kyvernoGood is the canonical "ship it" policy. The placeholder tokens let
// each test case make a single, unambiguous substring substitution rather than
// relying on heavyweight doc rewrites.
const kyvernoGood = `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-stellabill-images
spec:
  validationFailureAction: Enforce
  background: false
  rules:
    - name: require-cosign-and-slsa
      match:
        any:
          - resources:
              kinds: ["Pod"]
      verifyImages:
        - imageReferences:
            - "ghcr.io/stellabill/stellabill-backend*"
          required: true
          verifyDigest: true
          mutateDigest: true
          attestors:
            - count: 1
              entries:
                - keyless:
                    subject: "https://github.com/(Stellabill|believetimothy)/stellabill-backend/.github/workflows/release.yml@refs/tags/.+"
                    issuer:  "https://token.actions.githubusercontent.com"
          attestations:
            - predicateType: https://slsa.dev/provenance/v1
              attestors:
                - count: 1
                  entries:
                    - keyless:
                        subject: "https://github.com/(Stellabill|believetimothy)/stellabill-backend/.github/workflows/release.yml@refs/tags/.+"
                        issuer:  "https://token.actions.githubusercontent.com"
`

func TestValidateKyvernoPolicy_Success(t *testing.T) {
	cases := map[string][]byte{
		"happy":                              []byte(kyvernoGood),
		"kyverno v2 apiVersion":              []byte(strings.Replace(kyvernoGood, "kyverno.io/v1", "kyverno.io/v2", 1)),
		"Audit action is allowed":            []byte(strings.Replace(kyvernoGood, "validationFailureAction: Enforce", "validationFailureAction: Audit", 1)),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateKyvernoPolicy(doc); err != nil {
				t.Fatalf("expected success, got %v", err)
			}
		})
	}
}

func TestValidateKyvernoPolicy_Failures(t *testing.T) {
	// Each failure case ships a fully-formed YAML doc with one explicit
	// defect; we deliberately avoid the substring-replacement approch
	// because indentation in YAML makes it brittle.
	cases := []struct {
		name    string
		doc     string
		wantErr string
	}{
		{name: "empty", doc: "", wantErr: "empty policy document"},
		{name: "yaml parse error", doc: ":::bad yaml", wantErr: "parse policy yaml"},
		{
			name: "wrong apiVersion",
			doc: `apiVersion: kyverno.io/v3
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules: []
`,
			wantErr: "unexpected apiVersion",
		},
		{
			name: "wrong kind",
			doc: `apiVersion: kyverno.io/v1
kind: Policy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules: []
`,
			wantErr: "unexpected kind",
		},
		{
			name: "missing policy name",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: ""
spec:
  validationFailureAction: Enforce
  rules: []
`,
			wantErr: "metadata.name is empty",
		},
		{
			name: "missing validationFailureAction",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  rules: []
`,
			wantErr: "validationFailureAction is empty",
		},
		{
			name: "bad validationFailureAction",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Reject
  rules: []
`,
			wantErr: "must be Enforce or Audit",
		},
		{
			name: "no rules",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules: []
`,
			wantErr: "zero rules",
		},
		{
			name: "rule missing name",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules:
    - name: ""
      verifyImages: []
`,
			wantErr: "rule[0] missing name",
		},
		{
			name: "rule missing verifyImages",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules:
    - name: r
`,
			wantErr: "has no verifyImages block",
		},
		{
			name: "image ref not GHCR",
			doc: strings.Replace(kyvernoGood,
				`"ghcr.io/stellabill/stellabill-backend*"`,
				`"docker.io/library/nginx*"`, 1),
			wantErr: "must start with ghcr.io/",
		},
		{
			name: "image ref not stellabill",
			doc: strings.Replace(kyvernoGood,
				`"ghcr.io/stellabill/stellabill-backend*"`,
				`"ghcr.io/somebody/else*"`, 1),
			wantErr: "must cover ghcr.io/stellabill/",
		},
		{
			name: "required false",
			doc: strings.Replace(kyvernoGood, "required: true", "required: false", 1),
			wantErr: "must set required: true",
		},
		{
			name: "verifyDigest false",
			doc: strings.Replace(kyvernoGood, "verifyDigest: true", "verifyDigest: false", 1),
			wantErr: "must set verifyDigest: true",
		},
		{
			name: "no attestors",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules:
    - name: r
      verifyImages:
        - imageReferences: ["ghcr.io/stellabill/stellabill-backend*"]
          required: true
          verifyDigest: true
          mutateDigest: true
`,
			wantErr: "must declare at least one attestor",
		},
		{
			name: "attestor count zero",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules:
    - name: r
      verifyImages:
        - imageReferences: ["ghcr.io/stellabill/stellabill-backend*"]
          required: true
          verifyDigest: true
          mutateDigest: true
          attestors:
            - count: 0
              entries:
                - keyless:
                    subject: "https://github.com/Stellabill/stellabill-backend/.github/workflows/release.yml@refs/tags/.+"
                    issuer:  "https://token.actions.githubusercontent.com"
          attestations:
            - predicateType: https://slsa.dev/provenance/v1
`,
			wantErr: ".count must be >= 1",
		},
		{
			name: "no attestations block",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules:
    - name: r
      verifyImages:
        - imageReferences: ["ghcr.io/stellabill/stellabill-backend*"]
          required: true
          verifyDigest: true
          mutateDigest: true
          attestors:
            - count: 1
              entries:
                - keyless:
                    subject: "https://github.com/Stellabill/stellabill-backend/.github/workflows/release.yml@refs/tags/.+"
                    issuer:  "https://token.actions.githubusercontent.com"
`,
			wantErr: "must declare an SLSA attestations block",
		},
		{
			name: "wrong predicateType",
			doc: strings.Replace(kyvernoGood,
				"https://slsa.dev/provenance/v1",
				"https://in-toto.io/Statement/v0.1", 1),
			wantErr: "must be https://slsa.dev/provenance/v1",
		},
		{
			name: "subject wildcard org .+",
			doc: strings.Replace(kyvernoGood,
				"(Stellabill|believetimothy)", ".+", 1),
			wantErr: "wildcard GitHub org",
		},
		{
			name: "subject wildcard org .*",
			doc: strings.Replace(kyvernoGood,
				"(Stellabill|believetimothy)", ".*", 1),
			wantErr: "wildcard GitHub org",
		},
		{
			name: "attestor entry with empty subject (defensive)",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules:
    - name: r
      verifyImages:
        - imageReferences: ["ghcr.io/stellabill/stellabill-backend*"]
          required: true
          verifyDigest: true
          mutateDigest: true
          attestors:
            - count: 1
              entries:
                - keyless:
                    subject: ""
                    issuer: "https://token.actions.githubusercontent.com"
          attestations:
            - predicateType: https://slsa.dev/provenance/v1
              attestors:
                - count: 1
                  entries:
                    - keyless:
                        subject: "https://github.com/Stellabill/stellabill-backend/.github/workflows/release.yml@refs/tags/.+"
                        issuer: "https://token.actions.githubusercontent.com"
`,
			wantErr: "subject is empty",
		},
		{
			name: "empty imageReferences explicitly",
			doc: `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: x
spec:
  validationFailureAction: Enforce
  rules:
    - name: r
      verifyImages:
        - imageReferences: []
          required: true
          verifyDigest: true
          mutateDigest: true
`,
			wantErr: "empty imageReferences",
		},
		{
			name: "subject on different workflow file",
			doc: strings.Replace(kyvernoGood, "release.yml", "build.yml", 1),
			wantErr: "must reference /stellabill-backend/.github/workflows/release.yml",
		},
		{
			name: "issuer non-https-scheme",
			doc: strings.Replace(kyvernoGood,
				"https://token.actions.githubusercontent.com",
				"http://token.actions.githubusercontent.com", 1),
			wantErr: "must be exactly https://token.actions.githubusercontent.com",
		},
		{
			name: "issuer unrecognised domain",
			doc: strings.Replace(kyvernoGood,
				"https://token.actions.githubusercontent.com",
				"https://example.com/token", 1),
			wantErr: "must be exactly https://token.actions.githubusercontent.com",
		},
		{
			name: "subject empty",
			doc: strings.Replace(kyvernoGood,
				`https://github.com/(Stellabill|believetimothy)/stellabill-backend/.github/workflows/release.yml@refs/tags/.+`,
				"", 2),
			wantErr: "subject is empty",
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := ValidateKyvernoPolicy([]byte(c.doc))
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("error %q does not contain %q", err.Error(), c.wantErr)
			}
		})
	}
}

// ─── ValidateGitHubWorkflow ───────────────────────────────────────────────────

const wfGood = `name: Release
permissions: {}
on:
  push:
    tags:
      - "v[0-9]+.[0-9]+.[0-9]+*"
jobs:
  sign:
    name: Cosign sign (keyless)
    permissions:
      contents: read
      packages: write
      id-token: write
    steps:
      - name: Sign
        run: cosign sign --yes ghcr.io/x/y@sha256:abc
`

func TestValidateGitHubWorkflow_Success(t *testing.T) {
	docs := map[string][]byte{
		"happy":                     []byte(wfGood),
		"non-signing job, no id-token": []byte("name: x\non:\n  push:\n    branches: [main]\njobs:\n  build:\n    permissions:\n      contents: read\n    steps:\n      - run: go build ./...\n"),
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			if err := ValidateGitHubWorkflow(doc); err != nil {
				t.Fatalf("expected success, got %v", err)
			}
		})
	}
}

func TestValidateGitHubWorkflow_Failures(t *testing.T) {
	cases := []struct {
		name    string
		doc     []byte
		wantErr string
	}{
		{name: "empty", doc: []byte(""), wantErr: "empty workflow document"},
		{name: "yaml bad", doc: []byte(":::unbalanced"), wantErr: "parse workflow yaml"},
		{name: "missing name", doc: []byte(strings.Replace(wfGood, "name: Release\n", "", 1)), wantErr: "workflow name missing"},
		{name: "no jobs at all", doc: []byte("name: x\non:\n  push:\n    branches: [main]\njobs: {}\n"), wantErr: "no jobs"},
		{name: "signing job without permissions block", doc: []byte(strings.Replace(wfGood, "    permissions:\n      contents: read\n      packages: write\n      id-token: write\n", "", 1)), wantErr: "permissions.id-token: write"},
		{name: "signing job with id-token read", doc: []byte(strings.Replace(wfGood, "      id-token: write", "      id-token: read", 1)), wantErr: "id-token must be 'write'"},
		{name: "signing job with packages read", doc: []byte(strings.Replace(wfGood, "      packages: write", "      packages: read", 1)), wantErr: "packages must be 'write'"},
		{name: "cosign sign in a job without id-token", doc: []byte("name: x\non:\n  push:\n    branches: [main]\njobs:\n  adhoc:\n    name: adhoc-build\n    permissions:\n      contents: read\n    steps:\n      - name: sign-it\n        run: cosign sign --yes ghcr.io/x/y@sha256:abc\n"), wantErr: "permissions.id-token: write"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := ValidateGitHubWorkflow(c.doc)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("expected error containing %q; got %v", c.wantErr, err)
			}
		})
	}
}

// ─── ValidateSLSAProvenance ───────────────────────────────────────────────────

func TestValidateSLSAProvenance_Success(t *testing.T) {
	if err := ValidateSLSAProvenance([]byte(`{
"_type": "https://in-toto.io/Statement/v1",
"predicateType": "https://slsa.dev/provenance/v1",
"subject": [{"name": "ghcr.io/stellabill/stellabill-backend", "digest": {"sha256": "` + strings.Repeat("a", 64) + `"}}],
"predicate": {
"buildDefinition": {"buildType": "https://github.com/actions/workflow/v1"},
"runDetails": {
"builder":  {"id": "https://github.com/actions/runner"},
"metadata": {"invocationId": "https://github.com/owner/repo/actions/runs/123"}
}
}
}`)); err != nil {
		t.Fatalf("happy path should pass: %v", err)
	}
}

func TestValidateSLSAProvenance_Failures(t *testing.T) {
	goodSha := strings.Repeat("a", 64)
	good := `{"_type": "https://in-toto.io/Statement/v1","predicateType": "https://slsa.dev/provenance/v1","subject":[{"name":"x","digest":{"sha256":"` + goodSha + `"}}],"predicate":{"buildDefinition":{"buildType":"https://github.com/actions/workflow/v1"},"runDetails":{"builder":{"id":"https://github.com/actions/runner"},"metadata":{"invocationId":"https://github.com/owner/repo/actions/runs/123"}}}}`

	cases := []struct {
		name    string
		doc     []byte
		wantErr string
	}{
		{name: "empty", doc: []byte(""), wantErr: "empty provenance document"},
		{name: "bad json", doc: []byte("{not json"), wantErr: "parse provenance json"},
		{name: "bad _type", doc: []byte(strings.Replace(good, `"https://in-toto.io/Statement/v1"`, `"https://in-toto.io/Statement/v0.1"`, 1)), wantErr: "_type must be https://in-toto.io/Statement/v1"},
		{name: "bad predicateType", doc: []byte(strings.Replace(good, `"https://slsa.dev/provenance/v1"`, `"https://example.com/provenance/v9"`, 1)), wantErr: "predicateType must be https://slsa.dev/provenance/v1"},
		{name: "empty subject name", doc: []byte(strings.Replace(good, `"name":"x"`, `"name":""`, 1)), wantErr: "subject[0].name is empty"},
		{name: "missing sha256", doc: []byte(strings.Replace(good, `"sha256":"`+goodSha+`"`, `"sha1":"deadbeef"`, 1)), wantErr: "missing or non-string"},
		{name: "wrong length sha256", doc: []byte(strings.Replace(good, goodSha, goodSha[:32], 1)), wantErr: "64 hex chars"},
		{name: "uppercase hex sha256", doc: []byte(strings.Replace(good, goodSha, strings.ToUpper(goodSha), 1)), wantErr: "non-lowercase-hex"},
		{name: "missing buildType", doc: []byte(strings.Replace(good, `"https://github.com/actions/workflow/v1"`, `""`, 1)), wantErr: "buildDefinition.buildType is empty"},
		{name: "missing builder.id", doc: []byte(strings.Replace(good, `"id":"https://github.com/actions/runner"`, `"id":""`, 1)), wantErr: "runDetails.builder.id is empty"},
		{name: "missing invocationId", doc: []byte(strings.Replace(good, `"invocationId":"https://github.com/owner/repo/actions/runs/123"`, `"invocationId":""`, 1)), wantErr: "runDetails.metadata.invocationId is empty"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			err := ValidateSLSAProvenance(c.doc)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("expected error containing %q; got %v", c.wantErr, err)
			}
		})
	}
}

// ─── helper tests ─────────────────────────────────────────────────────────────

func TestJobPermissionsHelper(t *testing.T) {
	if val, ok := jobPermissions(map[string]any{"id-token": "write"}, "id-token"); !ok || val != "write" {
		t.Fatalf("expected write, ok=true; got val=%q ok=%v", val, ok)
	}
	if _, ok := jobPermissions(map[string]any{"contents": "read"}, "id-token"); ok {
		t.Fatalf("missing key should be ok=false")
	}
	if _, ok := jobPermissions(map[string]any{}, "id-token"); ok {
		t.Fatalf("empty perms should be ok=false")
	}
	if val, ok := jobPermissions(map[string]any{"id-token": 42}, "id-token"); !ok || val != "42" {
		t.Fatalf("non-string perm should still report; got %q %v", val, ok)
	}
}

func TestIsLowerHex(t *testing.T) {
	for _, r := range "0123456789abcdef" {
		if !isLowerHex(r) {
			t.Fatalf("expected lower hex for %q", r)
		}
	}
	for _, r := range "ABCDEFghijklmnop!@" {
		if isLowerHex(r) {
			t.Fatalf("expected non-hex for %q", r)
		}
	}
}

func TestIsSigningJob(t *testing.T) {
	if !isSigningJob("cosign sign (keyless)", nil) {
		t.Fatal("name-based detection should match")
	}
	if isSigningJob("build only", nil) {
		t.Fatal("non-signing job should not match")
	}
	yes := []gitHubStep{{Run: "cosign sign --yes ghcr.io/x/y@sha:abc"}}
	if !isSigningJob("build", yes) {
		t.Fatal("step with 'cosign sign' should match")
	}
	att := []gitHubStep{{Run: "cosign attest --yes --type slsaprovenance ..."}}
	if !isSigningJob("build", att) {
		t.Fatal("step with 'cosign attest' should match")
	}
	no := []gitHubStep{{Run: "go test ./..."}}
	if isSigningJob("build", no) {
		t.Fatal("plain step should not match")
	}
}

// ─── Smoke tests for shipped files ────────────────────────────────────────────

// repoRoot walks up the directory tree until it finds go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Skipf("could not locate go.mod from %s; skipping repo-root smoke test", dir)
			return ""
		}
		dir = parent
	}
}

func TestShippedKyvernoPolicyValidates(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		return
	}
	data, err := LoadFile(filepath.Join(root, "deploy/policies/kyverno-verify-images.yaml"))
	if err != nil {
		t.Skipf("policy file not present: %v", err)
	}
	if err := ValidateKyvernoPolicy(data); err != nil {
		t.Fatalf("shipped policy failed validation: %v", err)
	}
}

func TestShippedReleaseWorkflowValidates(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		return
	}
	data, err := LoadFile(filepath.Join(root, ".github/workflows/release.yml"))
	if err != nil {
		t.Skipf("release workflow not present: %v", err)
	}
	if err := ValidateGitHubWorkflow(data); err != nil {
		t.Fatalf("shipped release workflow failed validation: %v", err)
	}
}
