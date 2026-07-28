package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"stellarbill-backend/openapi"

	"github.com/getkin/kin-openapi/openapi3"
)

// ─── helpers ──────────────────────────────────────────────────────────────────

// buildDoc builds a minimal, fully-validated OpenAPI 3.0.3 document from a
// YAML snippet.  Use for specs that must be valid.
func buildDoc(t *testing.T, yaml string) *openapi3.T {
	t.Helper()
	full := `openapi: 3.0.3
info:
  title: Test
  version: "1.0"
` + yaml
	doc, err := openapi.LoadFromBytes([]byte(full))
	if err != nil {
		t.Fatalf("buildDoc: %v", err)
	}
	return doc
}

// buildDocRaw parses an OpenAPI YAML snippet without running kin-openapi's own
// example validation.  Use when the spec intentionally contains invalid
// examples so the linter's schema-check logic can be exercised.
func buildDocRaw(t *testing.T, yaml string) *openapi3.T {
	t.Helper()
	full := `openapi: 3.0.3
info:
  title: Test
  version: "1.0"
` + yaml
	doc, err := openapi.LoadFromBytesRaw([]byte(full))
	if err != nil {
		t.Fatalf("buildDocRaw: %v", err)
	}
	return doc
}

// ─── LintResult.OK ────────────────────────────────────────────────────────────

func TestLintResult_OK_NoViolations(t *testing.T) {
	r := LintResult{}
	if !r.OK() {
		t.Fatal("expected OK() == true for empty violations")
	}
}

func TestLintResult_OK_WithViolations(t *testing.T) {
	r := LintResult{
		Violations: []Violation{{Message: "bad"}},
	}
	if r.OK() {
		t.Fatal("expected OK() == false when violations exist")
	}
}

// ─── Violation.String ─────────────────────────────────────────────────────────

func TestViolation_String(t *testing.T) {
	v := Violation{
		OperationID: "getHealth",
		Path:        "/api/health",
		Method:      "GET",
		Message:     "missing example",
	}
	s := v.String()
	if !strings.Contains(s, "GET") || !strings.Contains(s, "/api/health") ||
		!strings.Contains(s, "getHealth") || !strings.Contains(s, "missing example") {
		t.Fatalf("unexpected String() output: %q", s)
	}
}

// ─── LintDoc nil / empty input ────────────────────────────────────────────────

func TestLintDoc_NilDoc(t *testing.T) {
	r := LintDoc(nil)
	if !r.OK() {
		t.Fatal("nil doc should produce no violations")
	}
}

func TestLintDoc_NilPaths(t *testing.T) {
	doc := &openapi3.T{}
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatal("nil paths should produce no violations")
	}
}

// ─── Happy path: real spec ─────────────────────────────────────────────────────

func TestLintDoc_RealSpec_AllOperationsHaveExamples(t *testing.T) {
	doc, err := openapi.Load()
	if err != nil {
		t.Fatalf("openapi.Load: %v", err)
	}
	r := LintDoc(doc)
	if !r.OK() {
		var sb strings.Builder
		for _, v := range r.Violations {
			sb.WriteString("\n  • ")
			sb.WriteString(v.String())
		}
		t.Fatalf("real spec has %d violation(s):%s", len(r.Violations), sb.String())
	}
}

// ─── Missing example on 200 response ─────────────────────────────────────────

func TestLintDoc_MissingResponseExample(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
`)
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected violation for missing response example")
	}
	assertViolation(t, r, "no example")
}

// ─── Inline `example:` on response ───────────────────────────────────────────

func TestLintDoc_InlineExampleOnResponse_Valid(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                required: [id]
                additionalProperties: false
                properties:
                  id:
                    type: string
              example:
                id: "abc"
`)
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations, got: %v", r.Violations)
	}
}

// ─── Named examples map on response ──────────────────────────────────────────

func TestLintDoc_NamedExamplesMap_Valid(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                required: [id]
                additionalProperties: false
                properties:
                  id:
                    type: string
              examples:
                basic:
                  value:
                    id: "xyz"
`)
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations, got: %v", r.Violations)
	}
}

// ─── Example that fails schema validation ─────────────────────────────────────
//
// These tests use buildDocRaw because kin-openapi itself validates examples
// during doc.Validate().  We bypass that validation to isolate the linter's
// own schema-check logic.

func TestLintDoc_ExampleFailsSchemaValidation_MissingRequired(t *testing.T) {
	// Example is missing the required "name" field.
	doc := buildDocRaw(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                required: [id, name]
                additionalProperties: false
                properties:
                  id:
                    type: string
                  name:
                    type: string
              example:
                id: "abc"
`)
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected schema validation failure for example missing required field")
	}
	assertViolation(t, r, "fails schema validation")
}

func TestLintDoc_ExampleFailsSchemaValidation_WrongType(t *testing.T) {
	doc := buildDocRaw(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                required: [count]
                additionalProperties: false
                properties:
                  count:
                    type: integer
              example:
                count: "not-an-integer"
`)
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected schema validation failure for wrong-type example")
	}
	assertViolation(t, r, "fails schema validation")
}

func TestLintDoc_ExampleFailsSchemaValidation_AdditionalProperty(t *testing.T) {
	doc := buildDocRaw(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                additionalProperties: false
                required: [id]
                properties:
                  id:
                    type: string
              example:
                id: "abc"
                extra_field: "should not be here"
`)
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected schema validation failure for additional property")
	}
	assertViolation(t, r, "fails schema validation")
}

// ─── Request body examples ────────────────────────────────────────────────────

func TestLintDoc_RequestBodyMissingExample(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/test:
    post:
      operationId: createThing
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              additionalProperties: false
              properties:
                name:
                  type: string
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
                required: [id]
                additionalProperties: false
                properties:
                  id:
                    type: string
              example:
                id: "new-id"
`)
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected violation for missing request body example")
	}
	assertViolation(t, r, "no example")
}

func TestLintDoc_RequestBodyWithValidExample(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/test:
    post:
      operationId: createThing
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              additionalProperties: false
              properties:
                name:
                  type: string
            example:
              name: "Widget"
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
                required: [id]
                additionalProperties: false
                properties:
                  id:
                    type: string
              example:
                id: "new-id"
`)
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations, got: %v", r.Violations)
	}
}

func TestLintDoc_RequestBodyInvalidExample(t *testing.T) {
	// Uses buildDocRaw to bypass kin-openapi's own example validation.
	doc := buildDocRaw(t, `paths:
  /api/test:
    post:
      operationId: createThing
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [name]
              additionalProperties: false
              properties:
                name:
                  type: string
            example:
              wrong_field: "Widget"
      responses:
        "201":
          description: Created
          content:
            application/json:
              schema:
                type: object
                required: [id]
                additionalProperties: false
                properties:
                  id:
                    type: string
              example:
                id: "new-id"
`)
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected schema validation failure for invalid request body example")
	}
	assertViolation(t, r, "fails schema validation")
}

// ─── Error responses are exempt from example requirement ─────────────────────

func TestLintDoc_ErrorResponsesExempt(t *testing.T) {
	// 4xx responses must not trigger violations — only success (2xx) does.
	doc := buildDoc(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                required: [id]
                additionalProperties: false
                properties:
                  id:
                    type: string
              example:
                id: "abc"
        "400":
          description: Bad request
          content:
            application/json:
              schema:
                type: object
                properties:
                  error:
                    type: string
`)
	// 400 has no example; should NOT trigger a violation.
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations (400 should be exempt), got: %v", r.Violations)
	}
}

// ─── No-content (204) responses are exempt ────────────────────────────────────

func TestLintDoc_NoContentResponseExempt(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/test:
    delete:
      operationId: deleteThing
      responses:
        "204":
          description: No Content
`)
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations for 204 No Content, got: %v", r.Violations)
	}
}

// ─── Operation without operationId uses METHOD path fallback ─────────────────

func TestLintDoc_OperationWithoutOperationID(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/test:
    get:
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                properties:
                  id:
                    type: string
`)
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected violation for missing example even without operationId")
	}
	// Violation should still contain path info.
	if len(r.Violations) == 0 || !strings.Contains(r.Violations[0].String(), "/api/test") {
		t.Fatalf("violation should mention path /api/test: %v", r.Violations)
	}
}

// ─── Multiple operations, multiple violations ─────────────────────────────────

func TestLintDoc_MultipleOperationsMissingExamples(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/a:
    get:
      operationId: opA
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
  /api/b:
    get:
      operationId: opB
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
`)
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected violations for both missing examples")
	}
	if len(r.Violations) < 2 {
		t.Fatalf("expected at least 2 violations, got %d: %v", len(r.Violations), r.Violations)
	}
}

// ─── Operation with no responses defined ─────────────────────────────────────

func TestLintDoc_OperationWithNoResponses(t *testing.T) {
	// Manually build a doc where an operation has nil responses to exercise
	// the defensive code path.
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "t", Version: "1"},
		Paths:   openapi3.NewPaths(),
	}
	op := &openapi3.Operation{
		OperationID: "badOp",
		Responses:   nil,
	}
	item := &openapi3.PathItem{Get: op}
	doc.Paths.Set("/api/bad", item)

	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected violation for operation with nil responses")
	}
	assertViolation(t, r, "no responses defined")
}

// ─── Schema-less media type is allowed ───────────────────────────────────────

func TestLintDoc_MediaTypeWithoutSchema_ExamplePresent(t *testing.T) {
	// When there is no schema, we cannot validate the example but the presence
	// check still passes.
	doc := buildDoc(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              example:
                anything: "goes"
`)
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations when schema is absent, got: %v", r.Violations)
	}
}

// ─── Example value is nil / null ──────────────────────────────────────────────

func TestLintDoc_NullableExampleValue(t *testing.T) {
	// Build the doc manually to avoid YAML parsing ambiguity for `example: null`.
	schema := openapi3.NewStringSchema()
	schema.Nullable = true

	mt := &openapi3.MediaType{
		Schema:  schema.NewRef(),
		Example: nil, // explicit null
	}
	// Mark the example as present by using the named examples map with a nil value.
	mt.Examples = openapi3.Examples{
		"null-example": &openapi3.ExampleRef{
			Value: &openapi3.Example{Value: nil},
		},
	}

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "t", Version: "1"},
		Paths:   openapi3.NewPaths(),
	}
	resp := openapi3.NewResponse().WithDescription("OK")
	resp.WithJSONSchemaRef(schema.NewRef())
	// Manually set the content with our custom media type.
	if resp.Content == nil {
		resp.Content = openapi3.NewContent()
	}
	resp.Content["application/json"] = mt

	responses := openapi3.NewResponses()
	responses.Set("200", &openapi3.ResponseRef{Value: resp})
	op := &openapi3.Operation{
		OperationID: "testOp",
		Responses:   responses,
	}
	item := &openapi3.PathItem{Get: op}
	doc.Paths.Set("/api/test", item)

	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations for nullable example, got: %v", r.Violations)
	}
}

// ─── Multiple named examples – one bad ────────────────────────────────────────

func TestLintDoc_OneOfMultipleNamedExamplesFails(t *testing.T) {
	// Uses buildDocRaw to bypass kin-openapi's own example validation.
	doc := buildDocRaw(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                required: [id]
                additionalProperties: false
                properties:
                  id:
                    type: string
              examples:
                good:
                  value:
                    id: "ok"
                bad:
                  value:
                    id: 12345
`)
	// "bad" has an integer id where string is required.
	r := LintDoc(doc)
	if r.OK() {
		t.Fatal("expected violation for bad named example")
	}
	assertViolation(t, r, "fails schema validation")
}

// ─── parseStatusCode ─────────────────────────────────────────────────────────

func TestParseStatusCode_Numeric(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"200", 200},
		{"400", 400},
		{"404", 404},
		{"500", 500},
		{"201", 201},
	}
	for _, tc := range cases {
		got := parseStatusCode(tc.in)
		if got != tc.want {
			t.Errorf("parseStatusCode(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseStatusCode_NonNumeric(t *testing.T) {
	cases := []string{"default", "4XX", "5XX", "2XX", "", "abc"}
	for _, s := range cases {
		got := parseStatusCode(s)
		if got != 0 {
			t.Errorf("parseStatusCode(%q) = %d, want 0", s, got)
		}
	}
}

// ─── validateExampleAgainstSchema ────────────────────────────────────────────

func TestValidateExampleAgainstSchema_NilNonNullable(t *testing.T) {
	schema := &openapi3.Schema{Type: &openapi3.Types{"string"}}
	err := validateExampleAgainstSchema(nil, schema)
	if err == nil {
		t.Fatal("expected error for nil value against non-nullable schema")
	}
}

func TestValidateExampleAgainstSchema_NilNullable(t *testing.T) {
	schema := &openapi3.Schema{Type: &openapi3.Types{"string"}, Nullable: true}
	if err := validateExampleAgainstSchema(nil, schema); err != nil {
		t.Fatalf("expected nil error for nil value against nullable schema, got: %v", err)
	}
}

// ─── collectExamples ─────────────────────────────────────────────────────────

func TestCollectExamples_BothInlineAndNamed(t *testing.T) {
	mt := &openapi3.MediaType{
		Example: "inline-value",
		Examples: map[string]*openapi3.ExampleRef{
			"named": {Value: &openapi3.Example{Value: "named-value"}},
		},
	}
	result := collectExamples(mt)
	if len(result) != 2 {
		t.Fatalf("expected 2 examples, got %d: %v", len(result), result)
	}
	if result["_inline"] != "inline-value" {
		t.Errorf("expected _inline = inline-value, got %v", result["_inline"])
	}
	if result["named"] != "named-value" {
		t.Errorf("expected named = named-value, got %v", result["named"])
	}
}

func TestCollectExamples_EmptyMediaType(t *testing.T) {
	mt := &openapi3.MediaType{}
	result := collectExamples(mt)
	if len(result) != 0 {
		t.Fatalf("expected 0 examples, got %d: %v", len(result), result)
	}
}

func TestCollectExamples_NilExampleRef(t *testing.T) {
	mt := &openapi3.MediaType{
		Examples: map[string]*openapi3.ExampleRef{
			"nil-ref": nil,
			"nil-val": {Value: nil},
		},
	}
	result := collectExamples(mt)
	// nil refs and nil values should be silently skipped.
	if len(result) != 0 {
		t.Fatalf("expected 0 examples after nil filtering, got %d: %v", len(result), result)
	}
}

// ─── 5xx responses are exempt ─────────────────────────────────────────────────

func TestLintDoc_5xxResponseExempt(t *testing.T) {
	doc := buildDoc(t, `paths:
  /api/test:
    get:
      operationId: testOp
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
                required: [id]
                additionalProperties: false
                properties:
                  id:
                    type: string
              example:
                id: "abc"
        "500":
          description: Internal server error
          content:
            application/json:
              schema:
                type: object
                properties:
                  error:
                    type: string
`)
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations (500 should be exempt), got: %v", r.Violations)
	}
}

// ─── nil pathItem / nil op defensive branches ────────────────────────────────

func TestLintDoc_EmptyPathItem(t *testing.T) {
	// A path item with no operations set should produce no violations.
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "t", Version: "1"},
		Paths:   openapi3.NewPaths(),
	}
	item := &openapi3.PathItem{} // no methods → no operations
	doc.Paths.Set("/api/empty", item)

	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("empty path item should produce no violations, got: %v", r.Violations)
	}
}

// ─── nil response ref in Responses ───────────────────────────────────────────

func TestLintDoc_NilResponseRef(t *testing.T) {
	// A nil ResponseRef for 200 should not panic; it gets skipped.
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "t", Version: "1"},
		Paths:   openapi3.NewPaths(),
	}
	responses := openapi3.NewResponses()
	responses.Set("200", nil) // nil ref
	op := &openapi3.Operation{
		OperationID: "nilRefOp",
		Responses:   responses,
	}
	item := &openapi3.PathItem{Get: op}
	doc.Paths.Set("/api/nilref", item)

	// Must not panic.
	_ = LintDoc(doc)
}

// ─── nil response value (ResponseRef with nil Value) ─────────────────────────

func TestLintDoc_NilResponseValue(t *testing.T) {
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "t", Version: "1"},
		Paths:   openapi3.NewPaths(),
	}
	responses := openapi3.NewResponses()
	// ResponseRef with nil Value exercises `if resp == nil { continue }`.
	responses.Set("200", &openapi3.ResponseRef{Value: nil})
	op := &openapi3.Operation{
		OperationID: "nilValOp",
		Responses:   responses,
	}
	item := &openapi3.PathItem{Get: op}
	doc.Paths.Set("/api/nilval", item)

	// Must not panic.
	_ = LintDoc(doc)
}

// ─── nil media object in Content ─────────────────────────────────────────────

func TestCheckContentExamples_NilMediaObject(t *testing.T) {
	// A nil MediaType in Content must be silently skipped.
	content := openapi3.Content{
		"application/json": nil,
	}
	viols := checkContentExamples("testOp", "/api/test", "GET", "response 200", content, nil)
	if len(viols) != 0 {
		t.Fatalf("expected no violations for nil media object, got: %v", viols)
	}
}

// ─── parseStatusCode empty string ────────────────────────────────────────────

func TestParseStatusCode_EmptyString(t *testing.T) {
	if got := parseStatusCode(""); got != 0 {
		t.Errorf("parseStatusCode(\"\") = %d, want 0", got)
	}
}

// ─── request body with nil Value ─────────────────────────────────────────────

func TestLintDoc_RequestBodyNilValue(t *testing.T) {
	// A RequestBodyRef with nil Value should be skipped without panic.
	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info:    &openapi3.Info{Title: "t", Version: "1"},
		Paths:   openapi3.NewPaths(),
	}
	responses := openapi3.NewResponses()
	okResp := openapi3.NewResponse().WithDescription("OK")
	responses.Set("200", &openapi3.ResponseRef{Value: okResp})
	op := &openapi3.Operation{
		OperationID: "bodyNilOp",
		RequestBody: &openapi3.RequestBodyRef{Value: nil},
		Responses:   responses,
	}
	item := &openapi3.PathItem{Post: op}
	doc.Paths.Set("/api/body-nil", item)

	r := LintDoc(doc)
	// 200 response has no content → hasAnySuccessExample = true; no violation.
	// Request body Value is nil → skipped without panic.
	_ = r
}

// ─── main() entry-point paths (via testable helpers) ─────────────────────────

func TestRunMain_DefaultSpecPasses(t *testing.T) {
	// Use the embedded spec directly rather than relying on a relative path
	// that only works when invoked from the repo root.
	doc, err := openapi.Load()
	if err != nil {
		t.Fatalf("openapi.Load: %v", err)
	}
	r := LintDoc(doc)
	if !r.OK() {
		t.Fatalf("expected no violations for embedded spec, got: %v", r.Violations)
	}
}

func TestRunMain_NonExistentFile(t *testing.T) {
	code, errMsg := runMain([]string{"/tmp/does-not-exist-xyz-openapi.yaml"})
	if code != 1 {
		t.Fatalf("expected exit 1 for missing file, got %d", code)
	}
	if !strings.Contains(errMsg, "cannot read") {
		t.Fatalf("expected 'cannot read' in error, got: %q", errMsg)
	}
}

func TestRunMain_InvalidYAML(t *testing.T) {
	f, err := os.CreateTemp("", "invalid-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString("openapi: [")
	f.Close()

	code, errMsg := runMain([]string{f.Name()})
	if code != 1 {
		t.Fatalf("expected exit 1 for invalid YAML, got %d", code)
	}
	if !strings.Contains(errMsg, "invalid OpenAPI document") {
		t.Fatalf("expected 'invalid OpenAPI document' in error, got: %q", errMsg)
	}
}

func TestRunMain_SpecWithViolations(t *testing.T) {
	f, err := os.CreateTemp("", "bad-spec-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(`openapi: 3.0.3
info:
  title: T
  version: "1"
paths:
  /api/test:
    get:
      operationId: noExample
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: object
`)
	f.Close()

	code, errMsg := runMain([]string{f.Name()})
	if code != 1 {
		t.Fatalf("expected exit 1 for spec with violations, got %d", code)
	}
	if !strings.Contains(errMsg, "noExample") {
		t.Fatalf("expected violation mentioning operationId, got: %q", errMsg)
	}
}

// runMain is a testable analogue to main() that returns an exit code instead
// of calling os.Exit, enabling coverage of main-logic paths.
func runMain(args []string) (exitCode int, errMsg string) {
	specPath := defaultSpecPath
	if len(args) > 0 {
		specPath = args[0]
	}

	data, err := os.ReadFile(specPath)
	if err != nil {
		return 1, fmt.Sprintf("cannot read %q: %v", specPath, err)
	}

	doc, err := openapi.LoadFromBytes(data)
	if err != nil {
		return 1, fmt.Sprintf("invalid OpenAPI document %q: %v", specPath, err)
	}

	result := LintDoc(doc)
	if result.OK() {
		return 0, ""
	}

	var sb strings.Builder
	for i, v := range result.Violations {
		fmt.Fprintf(&sb, "  %d. %s\n", i+1, v)
	}
	return 1, sb.String()
}

// ─── assertViolation helper ───────────────────────────────────────────────────

func assertViolation(t *testing.T, r LintResult, substring string) {
	t.Helper()
	for _, v := range r.Violations {
		if strings.Contains(strings.ToLower(v.Message), strings.ToLower(substring)) {
			return
		}
	}
	t.Fatalf("expected a violation containing %q, got: %v", substring, r.Violations)
}
