// Package main implements the openapi-lint tool.
//
// It provides the core linting logic that verifies every OpenAPI operation has
// at least one request/response example and that every example validates
// against its declared schema.
//
// # Design
//
// The linter is intentionally dependency-light: it uses only the already-
// vendored kin-openapi library (already required by the main module) plus the
// standard library.  No code-generation or external tools are needed; the
// check is a plain `go run ./tools/openapi-lint`.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Violation describes a single linting problem for one operation.
type Violation struct {
	// OperationID is the operationId field, or "METHOD path" when absent.
	OperationID string
	// Path is the URL path of the operation.
	Path string
	// Method is the HTTP method (upper-case).
	Method string
	// Message is a human-readable description of the problem.
	Message string
}

func (v Violation) String() string {
	return fmt.Sprintf("[%s %s (%s)] %s", v.Method, v.Path, v.OperationID, v.Message)
}

// LintResult is the aggregated output of a lint run.
type LintResult struct {
	Violations []Violation
}

// OK returns true when no violations were found.
func (r LintResult) OK() bool { return len(r.Violations) == 0 }

// LintDoc validates all operations in doc.  It checks:
//  1. Every non-4xx/5xx response has at least one example.
//  2. Every request body (when present) has at least one example.
//  3. Every example value validates against the schema for its media type.
//
// Non-content responses (e.g. 204 No Content) are excluded from the example
// requirement because they carry no body.  Shared error responses ($ref to
// components/responses) are not checked per-operation because they are defined
// once and may be reused by many operations; checking them per-operation would
// produce duplicate noise.
func LintDoc(doc *openapi3.T) LintResult {
	var result LintResult

	if doc == nil || doc.Paths == nil {
		return result
	}

	for path, pathItem := range doc.Paths.Map() {
		if pathItem == nil {
			continue
		}
		ops := pathItem.Operations()
		for method, op := range ops {
			if op == nil {
				continue
			}
			opID := op.OperationID
			if opID == "" {
				opID = method + " " + path
			}

			// ── Request body check ────────────────────────────────────────
			if op.RequestBody != nil {
				rb := op.RequestBody.Value
				if rb != nil {
					viols := checkContentExamples(opID, path, method, "request body", rb.Content, doc)
					result.Violations = append(result.Violations, viols...)
				}
			}

			// ── Response check ────────────────────────────────────────────
			// We require examples on success responses (2xx). Reference-only
			// ($ref) responses in components are shared definitions; we skip
			// them to avoid false positives on error responses that are
			// legitimately shared via $ref.
			if op.Responses == nil {
				result.Violations = append(result.Violations, Violation{
					OperationID: opID,
					Path:        path,
					Method:      method,
					Message:     "operation has no responses defined",
				})
				continue
			}

			hasAnySuccessExample := false
			for statusStr, respRef := range op.Responses.Map() {
				if respRef == nil {
					continue
				}

				// Skip shared $ref error responses in the 4xx/5xx range.
				// We only mandate examples on success (2xx) responses.
				code := parseStatusCode(statusStr)
				if code >= 400 {
					continue
				}

				resp := respRef.Value
				if resp == nil {
					continue
				}

				if len(resp.Content) == 0 {
					// No-content response (e.g. 204); no example required.
					hasAnySuccessExample = true
					continue
				}

				viols := checkContentExamples(opID, path, method,
					fmt.Sprintf("response %s", statusStr), resp.Content, doc)
				if len(viols) == 0 {
					hasAnySuccessExample = true
				}
				result.Violations = append(result.Violations, viols...)
			}

			if !hasAnySuccessExample {
				result.Violations = append(result.Violations, Violation{
					OperationID: opID,
					Path:        path,
					Method:      method,
					Message:     "no success (2xx) response with at least one example found",
				})
			}
		}
	}

	return result
}

// checkContentExamples verifies that the given media-type map has at least one
// example and that each example validates against its schema.
func checkContentExamples(
	opID, path, method, location string,
	content openapi3.Content,
	_ *openapi3.T,
) []Violation {
	var viols []Violation

	for mediaType, mediaObj := range content {
		if mediaObj == nil {
			continue
		}

		// Collect all example values: prefer named examples map, fall back to
		// the top-level `example` field.
		examples := collectExamples(mediaObj)

		if len(examples) == 0 {
			viols = append(viols, Violation{
				OperationID: opID,
				Path:        path,
				Method:      method,
				Message: fmt.Sprintf(
					"%s media type %q has no example",
					location, mediaType,
				),
			})
			continue
		}

		// Validate each example value against the schema (when present).
		if mediaObj.Schema != nil && mediaObj.Schema.Value != nil {
			for name, value := range examples {
				if err := validateExampleAgainstSchema(value, mediaObj.Schema.Value); err != nil {
					viols = append(viols, Violation{
						OperationID: opID,
						Path:        path,
						Method:      method,
						Message: fmt.Sprintf(
							"%s media type %q example %q fails schema validation: %v",
							location, mediaType, name, err,
						),
					})
				}
			}
		}
	}

	return viols
}

// collectExamples returns a map from example-name → example-value for a
// MediaType object.  It merges:
//   - All entries in the `examples` map (named examples).
//   - The top-level `example` field, stored under the synthetic key "_inline".
func collectExamples(mt *openapi3.MediaType) map[string]interface{} {
	out := make(map[string]interface{})

	if mt.Example != nil {
		out["_inline"] = mt.Example
	}

	for name, exRef := range mt.Examples {
		if exRef == nil {
			continue
		}
		ex := exRef.Value
		if ex == nil {
			continue
		}
		out[name] = ex.Value
	}

	return out
}

// validateExampleAgainstSchema validates a single example value against an
// OpenAPI schema.  It round-trips through JSON to normalise the value (Go maps
// from YAML unmarshalling may use map[interface{}]interface{} keys which JSON
// handles correctly).
func validateExampleAgainstSchema(value interface{}, schema *openapi3.Schema) error {
	if value == nil {
		// A nil/null example is only valid when the schema allows null.
		if schema.Nullable {
			return nil
		}
		return errors.New("example is null but schema does not allow null")
	}

	// Round-trip through JSON to normalise the value.
	// json.Marshal cannot fail for well-formed example values (no channels,
	// funcs, or complex numbers appear in YAML-parsed documents).
	raw, _ := json.Marshal(value)
	var parsed interface{}
	_ = json.Unmarshal(raw, &parsed)

	// Use kin-openapi's built-in schema validation.
	return schema.VisitJSON(parsed)
}

// parseStatusCode converts an HTTP status string (e.g. "200", "4XX", "default")
// to an integer.  Wildcard codes (e.g. "4XX") and non-numeric values return 0.
func parseStatusCode(s string) int {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0
	}
	// Reject if any character is not a digit (handles "4XX", "default", etc.).
	code := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		code = code*10 + int(c-'0')
	}
	return code
}

// run is the testable core of main(): reads a spec file, lints it, prints
// results to stdout/stderr, and returns an exit code.  Separating this from
// main() lets tests drive the full entrypoint logic without forking a subprocess.
func run(args []string) int {
	specPath := defaultSpecPath
	if len(args) > 0 {
		specPath = args[0]
	}

	data, err := readFile(specPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "openapi-lint: cannot read %q: %v\n", specPath, err)
		return 1
	}

	doc, err := loadSpec(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "openapi-lint: invalid OpenAPI document %q: %v\n", specPath, err)
		return 1
	}

	result := LintDoc(doc)
	if result.OK() {
		fmt.Printf("openapi-lint: all operations in %q have valid examples ✓\n", specPath)
		return 0
	}

	fmt.Fprintf(os.Stderr, "openapi-lint: %d violation(s) found in %q:\n\n",
		len(result.Violations), specPath)
	for i, v := range result.Violations {
		fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, v)
	}
	fmt.Fprintln(os.Stderr)
	return 1
}
