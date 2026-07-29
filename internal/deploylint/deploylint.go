// Package deploylint provides static, programmatic invariants for the release
// pipeline artefacts shipped by this repository:
//
//   - .github/workflows/release.yml            (build, sign, attest, verify)
//   - deploy/policies/kyverno-verify-images.yaml (admission policy)
//   - build-time SLSA v1 provenance JSON        (one file per release)
//
// It exists so that `go test ./internal/deploylint/...` gives contributors a
// fast local-feedback loop without installing actionlint / kyverno CLI. The
// validators are deliberately conservative: they fail closed on any missing
// field that would weaken supply-chain guarantees in production.
package deploylint

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFile reads a file from disk and returns its raw bytes. Callers pass the
// bytes to one of the typed validators below; we keep loading separate so tests
// can inject inline fixtures without touching the filesystem.
func LoadFile(path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("deploylint: empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("deploylint: read %q: %w", path, err)
	}
	return data, nil
}

// ─── Kyverno policy validation ───────────────────────────────────────────────

// KyvernoPolicy is a minimal structural model of the policy shipped in
// deploy/policies/kyverno-verify-images.yaml. We intentionally decode only the
// fields we must check — Kyverno has many other fields (autogen, conditions,
// exclude resources, …) and we want this code to keep working if those evolve.
type KyvernoPolicy struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   map[string]any `yaml:"metadata"`
	Spec       kyvernoSpec    `yaml:"spec"`
}

type kyvernoSpec struct {
	ValidationFailureAction string          `yaml:"validationFailureAction"`
	Background             bool            `yaml:"background"`
	Rules                  []kyvernoRule   `yaml:"rules"`
}

type kyvernoRule struct {
	Name         string              `yaml:"name"`
	Match        map[string]any      `yaml:"match"`
	VerifyImages []verifyImagesEntry `yaml:"verifyImages"`
}

// verifyImagesEntry represents one element of `spec.rules[].verifyImages` in
// the shipped policy. Kyverno allows multiple verifyImages entries per rule;
// we ensure at least one covers the Stellabill image pattern.
type verifyImagesEntry struct {
	ImageReferences []string          `yaml:"imageReferences"`
	Required        bool              `yaml:"required"`
	VerifyDigest    bool              `yaml:"verifyDigest"`
	MutateDigest    bool              `yaml:"mutateDigest"`
	Attestors       []kyvernoAttestor `yaml:"attestors"`
	Attestations    []kyvernoAttest   `yaml:"attestations"`
}

type kyvernoAttestor struct {
	Count   int                 `yaml:"count"`
	Entries []kyvernoAttestorEntry `yaml:"entries"`
}

type kyvernoAttestorEntry struct {
	Keyless *kyvernoKeyless `yaml:"keyless"`
}

type kyvernoKeyless struct {
	Subject string         `yaml:"subject"`
	Issuer  string         `yaml:"issuer"`
	Rekor   map[string]any `yaml:"rekor"`
}

type kyvernoAttest struct {
	PredicateType string             `yaml:"predicateType"`
	Attestors     []kyvernoAttestor  `yaml:"attestors"`
}

// ValidateKyvernoPolicy parses and statically verifies a ClusterPolicy used
// to enforce cosign + SLSA provenance at admission. The errors returned are
// safe to surface to operators: each error pins a specific invariant and the
// human-readable file location is included where possible.
func ValidateKyvernoPolicy(data []byte) error {
	if len(data) == 0 {
		return errors.New("deploylint: empty policy document")
	}
	var p KyvernoPolicy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("deploylint: parse policy yaml: %w", err)
	}
	if err := validatePolicy(&p); err != nil {
		return err
	}
	return nil
}

func validatePolicy(p *KyvernoPolicy) error {
	if p.APIVersion != "kyverno.io/v1" && p.APIVersion != "kyverno.io/v2" {
		return fmt.Errorf("deploylint: unexpected apiVersion %q (expected kyverno.io/v1)", p.APIVersion)
	}
	if p.Kind != "ClusterPolicy" {
		return fmt.Errorf("deploylint: unexpected kind %q (expected ClusterPolicy)", p.Kind)
	}
	if name, _ := p.Metadata["name"].(string); name == "" {
		return errors.New("deploylint: policy metadata.name is empty")
	}
	if p.Spec.ValidationFailureAction == "" {
		return errors.New("deploylint: spec.validationFailureAction is empty (must be Enforce or Audit)")
	}
	switch p.Spec.ValidationFailureAction {
	case "Enforce", "Audit":
	default:
		return fmt.Errorf("deploylint: validationFailureAction=%q (must be Enforce or Audit)", p.Spec.ValidationFailureAction)
	}
	if len(p.Spec.Rules) == 0 {
		return errors.New("deploylint: policy defines zero rules")
	}
	for i, rule := range p.Spec.Rules {
		if rule.Name == "" {
			return fmt.Errorf("deploylint: rule[%d] missing name", i)
		}
		if len(rule.VerifyImages) == 0 {
			return fmt.Errorf("deploylint: rule[%d] (%s) has no verifyImages block", i, rule.Name)
		}
		for j, vi := range rule.VerifyImages {
			if err := validateVerifyImages(i, rule.Name, j, &vi); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateVerifyImages(ruleIdx int, ruleName string, entryIdx int, vi *verifyImagesEntry) error {
	if len(vi.ImageReferences) == 0 {
		return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] has empty imageReferences", ruleIdx, ruleName, entryIdx)
	}
	stellabillCovered := false
	for _, ref := range vi.ImageReferences {
		if !strings.HasPrefix(ref, "ghcr.io/") {
			return fmt.Errorf("deploylint: image reference %q must start with ghcr.io/", ref)
		}
		if strings.HasPrefix(strings.ToLower(ref), "ghcr.io/stellabill/") {
			stellabillCovered = true
		}
	}
	if !stellabillCovered {
		return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] must cover ghcr.io/stellabill/* images", ruleIdx, ruleName, entryIdx)
	}
	if !vi.Required {
		return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] must set required: true", ruleIdx, ruleName, entryIdx)
	}
	if !vi.VerifyDigest {
		return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] must set verifyDigest: true", ruleIdx, ruleName, entryIdx)
	}
	if len(vi.Attestors) == 0 {
		return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] must declare at least one attestor", ruleIdx, ruleName, entryIdx)
	}
	if err := validateAttestors(ruleIdx, ruleName, entryIdx, "attestors", vi.Attestors); err != nil {
		return err
	}
	if len(vi.Attestations) == 0 {
		return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] must declare an SLSA attestations block", ruleIdx, ruleName, entryIdx)
	}
	for k, att := range vi.Attestations {
		if att.PredicateType != "https://slsa.dev/provenance/v1" {
			return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d].attestations[%d] must be https://slsa.dev/provenance/v1", ruleIdx, ruleName, entryIdx, k)
		}
		if err := validateAttestors(ruleIdx, ruleName, entryIdx, fmt.Sprintf("attestations[%d].attestors", k), att.Attestors); err != nil {
			return err
		}
	}
	return nil
}

func validateAttestors(ruleIdx int, ruleName string, entryIdx int, where string, list []kyvernoAttestor) error {
	for k, at := range list {
		if at.Count < 1 {
			return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] %s[%d].count must be >= 1", ruleIdx, ruleName, entryIdx, where, k)
		}
		if len(at.Entries) == 0 {
			return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] %s[%d] has no entries", ruleIdx, ruleName, entryIdx, where, k)
		}
		for e, ent := range at.Entries {
			if ent.Keyless == nil {
				return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] %s[%d].entries[%d] must be a keyless block", ruleIdx, ruleName, entryIdx, where, k, e)
			}
			if ent.Keyless.Issuer != "https://token.actions.githubusercontent.com" {
				return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] %s[%d].entries[%d].issuer must be exactly https://token.actions.githubusercontent.com (GitHub OIDC Fulcio), got %q", ruleIdx, ruleName, entryIdx, where, k, e, ent.Keyless.Issuer)
			}
			if ent.Keyless.Subject == "" {
				return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] %s[%d].entries[%d].subject is empty", ruleIdx, ruleName, entryIdx, where, k, e)
			}
			// The subject MUST pin to the exact workflow file we control AND a
			// known-good org. With `/.+/` for the org path, any GitHub user with
			// a repo called `stellabill-backend` and a `.github/workflows/release.yml`
			// could sign a malicious image and pass admission.
			if !strings.Contains(ent.Keyless.Subject, "/stellabill-backend/.github/workflows/release.yml") {
				return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] %s[%d].entries[%d].subject must reference /stellabill-backend/.github/workflows/release.yml", ruleIdx, ruleName, entryIdx, where, k, e)
			}
			if strings.Contains(ent.Keyless.Subject, "github.com/.*/stellabill-backend") ||
				strings.Contains(ent.Keyless.Subject, "github.com/.+/stellabill-backend") {
				return fmt.Errorf("deploylint: rule[%d] (%s) verifyImages[%d] %s[%d].entries[%d].subject uses a wildcard GitHub org \u2014 an attacker could publish a repo named stellabill-backend and impersonate us", ruleIdx, ruleName, entryIdx, where, k, e)
			}
		}
	}
	return nil
}

// ─── GitHub Actions workflow validation ──────────────────────────────────────

// GitHubWorkflow is a minimal structural model used to assert that the
// release workflow declares the right permissions for OIDC keyless signing.
type GitHubWorkflow struct {
	Name        string                    `yaml:"name"`
	On          any                       `yaml:"on"`
	Permissions any                       `yaml:"permissions"`
	Jobs        map[string]gitHubJob      `yaml:"jobs"`
}

type gitHubJob struct {
	Name        string         `yaml:"name"`
	Permissions map[string]any `yaml:"permissions"`
	Steps       []gitHubStep   `yaml:"steps"`
}

type gitHubStep struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	Env  map[string]any `yaml:"env"`
}

// isSigningJob returns true if a job name or step looks like a cosign sign /
// attest step. We use a small heuristic instead of parsing every step so we
// only flag the jobs that actually need `id-token: write`.
func isSigningJob(name string, steps []gitHubStep) bool {
	low := strings.ToLower(name)
	if strings.Contains(low, "sign") {
		return true
	}
	for _, s := range steps {
		hay := strings.ToLower(s.Name + " " + s.Run + " " + s.Uses)
		if strings.Contains(hay, "cosign sign") ||
			strings.Contains(hay, "cosign attest") {
			return true
		}
	}
	return false
}

// ValidateGitHubWorkflow parses a workflow file and asserts the permissions
// needed for OIDC keyless signing. This is the most common foot-gun: a
// workflow with `cosign sign` but no `id-token: write` will fail at signing
// time, not at PR time, so we catch it here.
func ValidateGitHubWorkflow(data []byte) error {
	if len(data) == 0 {
		return errors.New("deploylint: empty workflow document")
	}
	var wf GitHubWorkflow
	if err := yaml.Unmarshal(data, &wf); err != nil {
		return fmt.Errorf("deploylint: parse workflow yaml: %w", err)
	}
	if err := validateWorkflow(&wf); err != nil {
		return err
	}
	return nil
}

func validateWorkflow(wf *GitHubWorkflow) error {
	if wf.Name == "" {
		return errors.New("deploylint: workflow name missing")
	}
	if len(wf.Jobs) == 0 {
		return errors.New("deploylint: workflow has no jobs")
	}
	for jobName, job := range wf.Jobs {
		if !isSigningJob(job.Name, job.Steps) {
			continue
		}
		idToken, ok := jobPermissions(job.Permissions, "id-token")
		if !ok {
			return fmt.Errorf("deploylint: job %q performs cosign sign/attest but does not declare permissions.id-token: write — keyless signing will fail at runtime", jobName)
		}
		if idToken != "write" {
			return fmt.Errorf("deploylint: job %q permissions.id-token must be 'write', got %q", jobName, idToken)
		}
		if pkgs, ok := jobPermissions(job.Permissions, "packages"); ok && pkgs != "write" {
			return fmt.Errorf("deploylint: job %q permissions.packages must be 'write' to push the .sig/.att layers, got %q", jobName, pkgs)
		}
	}
	return nil
}

// jobPermissions returns the value for a given key from a permissions block,
// which GitHub Actions documents as a key:string map at the per-job level.
func jobPermissions(perms map[string]any, key string) (string, bool) {
	if len(perms) == 0 {
		return "", false
	}
	v, ok := perms[key]
	if !ok {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	return fmt.Sprintf("%v", v), true
}

// ─── SLSA provenance validation ───────────────────────────────────────────────

// SLSAProvenance is a minimal structural model of an in-toto statement that
// wraps an SLSA v1 provenance predicate. Only the fields the Kyverno policy
// inspects are kept.
type SLSAProvenance struct {
	Type          string             `json:"_type"`
	PredicateType string             `json:"predicateType"`
	Subject       []slsaSubject      `json:"subject"`
	Predicate     slsaPredicate      `json:"predicate"`
}

type slsaSubject struct {
	Name   string         `json:"name"`
	Digest map[string]any `json:"digest"`
}

type slsaPredicate struct {
	BuildDefinition slsaBuildDefinition `json:"buildDefinition"`
	RunDetails       slsaRunDetails      `json:"runDetails"`
}

type slsaBuildDefinition struct {
	BuildType          string         `json:"buildType"`
	ExternalParameters map[string]any `json:"externalParameters"`
	ResolvedDependencies []map[string]any `json:"resolvedDependencies"`
}

type slsaRunDetails struct {
	Builder  map[string]any `json:"builder"`
	Metadata map[string]any `json:"metadata"`
}

// ValidateSLSAProvenance asserts that a JSON file is structurally sound and
// contains the minimum fields Kyverno and downstream auditors will look at.
func ValidateSLSAProvenance(data []byte) error {
	if len(data) == 0 {
		return errors.New("deploylint: empty provenance document")
	}
	var p SLSAProvenance
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("deploylint: parse provenance json: %w", err)
	}
	if p.Type != "https://in-toto.io/Statement/v1" {
		return fmt.Errorf("deploylint: provenance _type must be https://in-toto.io/Statement/v1, got %q", p.Type)
	}
	if p.PredicateType != "https://slsa.dev/provenance/v1" {
		return fmt.Errorf("deploylint: provenance predicateType must be https://slsa.dev/provenance/v1, got %q", p.PredicateType)
	}
	if len(p.Subject) != 1 {
		return fmt.Errorf("deploylint: provenance subject must contain exactly one entry, got %d", len(p.Subject))
	}
	if p.Subject[0].Name == "" {
		return errors.New("deploylint: provenance subject[0].name is empty")
	}
	sha, ok := p.Subject[0].Digest["sha256"].(string)
	if !ok || sha == "" {
		return errors.New("deploylint: provenance subject[0].digest.sha256 is missing or non-string")
	}
	if len(sha) != 64 {
		return fmt.Errorf("deploylint: provenance subject[0].digest.sha256 must be 64 hex chars (got %d)", len(sha))
	}
	for _, ch := range sha {
		if !isLowerHex(ch) {
			return errors.New("deploylint: provenance subject[0].digest.sha256 contains non-lowercase-hex characters")
		}
	}
	if p.Predicate.BuildDefinition.BuildType == "" {
		return errors.New("deploylint: provenance predicate.buildDefinition.buildType is empty")
	}
	id, ok := p.Predicate.RunDetails.Builder["id"]
	if !ok || id == nil || asString(id) == "" {
		return errors.New("deploylint: provenance predicate.runDetails.builder.id is empty")
	}
	inv, ok := p.Predicate.RunDetails.Metadata["invocationId"]
	if !ok || inv == nil || asString(inv) == "" {
		return errors.New("deploylint: provenance predicate.runDetails.metadata.invocationId is empty")
	}
	return nil
}

// asString is a defensive type-coercion for untyped JSON-decoded fields: the
// JSON parser keeps strings as string, but a hand-rolled payload could put an
// int or bool here.
func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// isLowerHex returns true for lowercase hex digits (0-9, a-f).
func isLowerHex(r rune) bool {
	switch {
	case r >= '0' && r <= '9':
		return true
	case r >= 'a' && r <= 'f':
		return true
	default:
		return false
	}
}

// ─── Convenience helpers for tests ───────────────────────────────────────────

// ValidImageRef is the production stellabill image reference used by the
// shipped policy. Exposed for test fixtures so changes here propagate to test
// expectations automatically.
const ValidImageRef = "ghcr.io/stellabill/stellabill-backend"

// ValidSLSAImageRef returns a normalized form of ValidImageRef useful for
// assertions that need to round-trip through the YAML parser (mixed case is
// normalised by GHCR, so we lowercase before comparison).
func ValidSLSAImageRef(name string) string {
	return strings.ToLower(name)
}
