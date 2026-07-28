package jsonx_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"stellarbill-backend/internal/jsonx"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

type samplePlan struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
	Description string  `json:"description"`
}

type sampleStatement struct {
	ID         string      `json:"id"`
	CustomerID string      `json:"customer_id"`
	Amount     float64     `json:"amount"`
	Currency   string      `json:"currency"`
	Meta       interface{} `json:"meta,omitempty"`
}

var planFixture = samplePlan{
	ID:          "plan-001",
	Name:        "Starter",
	Amount:      9.99,
	Currency:    "USD",
	Description: "Entry-level plan",
}

var stmtFixture = sampleStatement{
	ID:         "stmt-abc",
	CustomerID: "cust-xyz",
	Amount:     149.50,
	Currency:   "GBP",
}

// htmlFixture carries values that contain HTML-special characters to test
// that the active implementation handles them safely for API JSON responses.
// (stdlib escapes <, >, & by default; sonic does not — both are correct for
// application/json responses but we document and verify the difference.)
var htmlFixture = struct {
	URL  string `json:"url"`
	Note string `json:"note"`
}{
	URL:  "https://example.com/path?a=1&b=2",
	Note: "<b>bold</b> & special",
}

// ---------------------------------------------------------------------------
// Marshal / Unmarshal round-trip
// ---------------------------------------------------------------------------

func TestMarshal_RoundTrip_Plan(t *testing.T) {
	data, err := jsonx.Marshal(planFixture)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var got samplePlan
	if err := jsonx.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if got != planFixture {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, planFixture)
	}
}

func TestMarshal_RoundTrip_Statement(t *testing.T) {
	data, err := jsonx.Marshal(stmtFixture)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	var got sampleStatement
	if err := jsonx.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if got != stmtFixture {
		t.Fatalf("round-trip mismatch: got %+v want %+v", got, stmtFixture)
	}
}

func TestMarshal_NilValue(t *testing.T) {
	data, err := jsonx.Marshal(nil)
	if err != nil {
		t.Fatalf("Marshal(nil) error: %v", err)
	}
	if string(data) != "null" {
		t.Fatalf("Marshal(nil) = %q, want \"null\"", data)
	}
}

func TestMarshal_EmptySlice(t *testing.T) {
	data, err := jsonx.Marshal([]samplePlan{})
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(data) != "[]" {
		t.Fatalf("Marshal([]) = %q, want \"[]\"", data)
	}
}

func TestMarshal_LargeSlice(t *testing.T) {
	plans := make([]samplePlan, 200)
	for i := range plans {
		plans[i] = planFixture
	}
	data, err := jsonx.Marshal(plans)
	if err != nil {
		t.Fatalf("Marshal large slice error: %v", err)
	}
	var got []samplePlan
	if err := jsonx.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal large slice error: %v", err)
	}
	if len(got) != 200 {
		t.Fatalf("large slice round-trip: got %d items, want 200", len(got))
	}
}

func TestMarshalIndent(t *testing.T) {
	data, err := jsonx.MarshalIndent(planFixture, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent error: %v", err)
	}
	// Verify the indented output is valid JSON and round-trips correctly.
	var got samplePlan
	if err := jsonx.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal indented error: %v", err)
	}
	if got != planFixture {
		t.Fatalf("MarshalIndent round-trip mismatch")
	}
	if !bytes.Contains(data, []byte("\n")) {
		t.Fatal("MarshalIndent output has no newlines")
	}
}

// ---------------------------------------------------------------------------
// HTML-safety equivalence
//
// For application/json HTTP responses the active encoder need not HTML-escape
// <, >, & — the Content-Type boundary prevents XSS. We verify:
//   1. The output is valid JSON that round-trips correctly.
//   2. The semantic values are preserved regardless of whether the
//      implementation chooses to escape or not.
// ---------------------------------------------------------------------------

func TestHTMLChars_SemanticPreservation(t *testing.T) {
	data, err := jsonx.Marshal(htmlFixture)
	if err != nil {
		t.Fatalf("Marshal HTML fixture error: %v", err)
	}

	// Must be valid JSON.
	var got struct {
		URL  string `json:"url"`
		Note string `json:"note"`
	}
	if err := jsonx.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal HTML fixture error: %v", err)
	}

	// Semantic values must be identical to the originals after decode.
	if got.URL != htmlFixture.URL {
		t.Fatalf("URL mismatch: got %q want %q", got.URL, htmlFixture.URL)
	}
	if got.Note != htmlFixture.Note {
		t.Fatalf("Note mismatch: got %q want %q", got.Note, htmlFixture.Note)
	}
}

// TestHTMLChars_StdlibEquivalence verifies that jsonx and encoding/json produce
// semantically equivalent output for HTML-bearing values (decoded values match),
// even if the raw bytes differ due to escaping policy.
func TestHTMLChars_StdlibEquivalence(t *testing.T) {
	jsonxBytes, err := jsonx.Marshal(htmlFixture)
	if err != nil {
		t.Fatalf("jsonx.Marshal error: %v", err)
	}
	stdlibBytes, err := json.Marshal(htmlFixture)
	if err != nil {
		t.Fatalf("json.Marshal error: %v", err)
	}

	type out struct {
		URL  string `json:"url"`
		Note string `json:"note"`
	}
	var fromJsonx, fromStdlib out
	if err := json.Unmarshal(jsonxBytes, &fromJsonx); err != nil {
		t.Fatalf("decode jsonx output with stdlib: %v", err)
	}
	if err := json.Unmarshal(stdlibBytes, &fromStdlib); err != nil {
		t.Fatalf("decode stdlib output with stdlib: %v", err)
	}
	if fromJsonx != fromStdlib {
		t.Fatalf("semantic mismatch: jsonx=%+v stdlib=%+v", fromJsonx, fromStdlib)
	}
}

// ---------------------------------------------------------------------------
// Streaming encoder / decoder
// ---------------------------------------------------------------------------

func TestNewEncoder_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := jsonx.NewEncoder(&buf)
	if err := enc.Encode(planFixture); err != nil {
		t.Fatalf("Encode error: %v", err)
	}

	var got samplePlan
	dec := jsonx.NewDecoder(&buf)
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if got != planFixture {
		t.Fatalf("encoder round-trip mismatch: got %+v want %+v", got, planFixture)
	}
}

func TestNewEncoder_MultipleValues(t *testing.T) {
	var buf bytes.Buffer
	enc := jsonx.NewEncoder(&buf)
	plans := []samplePlan{planFixture, planFixture}
	for _, p := range plans {
		if err := enc.Encode(p); err != nil {
			t.Fatalf("Encode error: %v", err)
		}
	}

	dec := jsonx.NewDecoder(&buf)
	var decoded []samplePlan
	for dec.More() {
		var p samplePlan
		if err := dec.Decode(&p); err != nil {
			break
		}
		decoded = append(decoded, p)
	}
	if len(decoded) != 2 {
		t.Fatalf("expected 2 decoded values, got %d", len(decoded))
	}
}

func TestNewDecoder_UseNumber(t *testing.T) {
	dec := jsonx.NewDecoder(strings.NewReader(`{"amount":9.99}`))
	dec.UseNumber()
	var m map[string]interface{}
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if _, ok := m["amount"].(json.Number); !ok {
		t.Fatalf("UseNumber: expected json.Number, got %T", m["amount"])
	}
}

// ---------------------------------------------------------------------------
// ConfigName
// ---------------------------------------------------------------------------

func TestConfigName_ValidValue(t *testing.T) {
	name := jsonx.ConfigName()
	if name != "sonic" && name != "stdlib" {
		t.Fatalf("unexpected ConfigName: %q", name)
	}
}

// ---------------------------------------------------------------------------
// Benchmarks — compare jsonx vs encoding/json on statement-list payloads
// ---------------------------------------------------------------------------

func BenchmarkMarshal_Statement_jsonx(b *testing.B) {
	stmts := make([]sampleStatement, 20)
	for i := range stmts {
		stmts[i] = stmtFixture
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := jsonx.Marshal(stmts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal_Statement_stdlib(b *testing.B) {
	stmts := make([]sampleStatement, 20)
	for i := range stmts {
		stmts[i] = stmtFixture
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(stmts); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal_Plans_jsonx(b *testing.B) {
	plans := make([]samplePlan, 50)
	for i := range plans {
		plans[i] = planFixture
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := jsonx.Marshal(plans); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal_Plans_stdlib(b *testing.B) {
	plans := make([]samplePlan, 50)
	for i := range plans {
		plans[i] = planFixture
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(plans); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_Statement_jsonx(b *testing.B) {
	data, _ := json.Marshal(stmtFixture)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v sampleStatement
		if err := jsonx.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_Statement_stdlib(b *testing.B) {
	data, _ := json.Marshal(stmtFixture)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var v sampleStatement
		if err := json.Unmarshal(data, &v); err != nil {
			b.Fatal(err)
		}
	}
}
