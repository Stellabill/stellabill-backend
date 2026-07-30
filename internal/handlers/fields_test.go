package handlers

import (
	"encoding/json"
	"testing"
)

type testProjectionStruct struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Amount int    `json:"amount"`
	Secret string `json:"-"`
}

type testNestedStruct struct {
	ID   string `json:"id"`
	Meta struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	} `json:"meta"`
}

func TestProjectFields_AllFields(t *testing.T) {
	v := testProjectionStruct{ID: "abc", Name: "test", Amount: 100}
	result, err := ProjectFields(v, []string{"id", "name", "amount"})
	if err != nil {
		t.Fatalf("ProjectFields failed: %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(result))
	}
	if string(result["id"]) != `"abc"` {
		t.Errorf("expected id to be \"abc\", got %s", string(result["id"]))
	}
	if string(result["name"]) != `"test"` {
		t.Errorf("expected name to be \"test\", got %s", string(result["name"]))
	}
	if string(result["amount"]) != `100` {
		t.Errorf("expected amount to be 100, got %s", string(result["amount"]))
	}
}

func TestProjectFields_Subset(t *testing.T) {
	v := testProjectionStruct{ID: "abc", Name: "test", Amount: 100}
	result, err := ProjectFields(v, []string{"id", "name"})
	if err != nil {
		t.Fatalf("ProjectFields failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(result))
	}
	if _, ok := result["amount"]; ok {
		t.Error("expected amount to be excluded")
	}
}

func TestProjectFields_SingleField(t *testing.T) {
	v := testProjectionStruct{ID: "abc", Name: "test", Amount: 100}
	result, err := ProjectFields(v, []string{"name"})
	if err != nil {
		t.Fatalf("ProjectFields failed: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 field, got %d", len(result))
	}
	if string(result["name"]) != `"test"` {
		t.Errorf("expected name to be \"test\", got %s", string(result["name"]))
	}
}

func TestProjectFields_EmptyFieldsError(t *testing.T) {
	v := testProjectionStruct{ID: "abc"}
	_, err := ProjectFields(v, []string{})
	if err == nil {
		t.Fatal("expected error for empty fields")
	}
}

func TestProjectFields_UnexportedFieldExcluded(t *testing.T) {
	v := testProjectionStruct{ID: "abc", Name: "test", Secret: "hidden"}
	result, err := ProjectFields(v, []string{"secret"})
	if err != nil {
		t.Fatalf("ProjectFields failed: %v", err)
	}
	if _, ok := result["secret"]; ok {
		t.Error("expected unexported/json-skipped field to be absent")
	}
}

func TestProjectFields_NestedStruct(t *testing.T) {
	v := testNestedStruct{ID: "xyz"}
	v.Meta.Key = "k1"
	v.Meta.Value = "v1"
	result, err := ProjectFields(v, []string{"id", "meta"})
	if err != nil {
		t.Fatalf("ProjectFields failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(result))
	}
	if string(result["id"]) != `"xyz"` {
		t.Errorf("expected id to be \"xyz\", got %s", string(result["id"]))
	}
	// Verify meta is a nested object.
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(result["meta"], &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if string(meta["key"]) != `"k1"` {
		t.Errorf("expected meta.key to be \"k1\", got %s", string(meta["key"]))
	}
}

func TestProjectFields_JSONRoundTrip(t *testing.T) {
	v := testProjectionStruct{ID: "abc", Name: "test", Amount: 100}
	result, err := ProjectFields(v, []string{"id", "amount"})
	if err != nil {
		t.Fatalf("ProjectFields failed: %v", err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal projected: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal roundtrip: %v", err)
	}
	// Should only have id and amount.
	if len(decoded) != 2 {
		t.Fatalf("expected 2 fields after roundtrip, got %d", len(decoded))
	}
}

func TestProjectSlice(t *testing.T) {
	items := []testProjectionStruct{
		{ID: "a", Name: "A", Amount: 1},
		{ID: "b", Name: "B", Amount: 2},
	}
	result, err := ProjectSlice(items, []string{"id", "name"})
	if err != nil {
		t.Fatalf("ProjectSlice failed: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}
	for i, projected := range result {
		if len(projected) != 2 {
			t.Errorf("item %d: expected 2 fields, got %d", i, len(projected))
		}
		if _, ok := projected["amount"]; ok {
			t.Errorf("item %d: expected amount to be excluded", i)
		}
	}
}

func TestProjectSlice_Empty(t *testing.T) {
	result, err := ProjectSlice([]testProjectionStruct{}, []string{"id"})
	if err != nil {
		t.Fatalf("ProjectSlice failed: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d items", len(result))
	}
}

func TestProjectSlice_NilItems(t *testing.T) {
	var items []testProjectionStruct
	result, err := ProjectSlice(items, []string{"id"})
	if err != nil {
		t.Fatalf("ProjectSlice failed: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty result, got %d items", len(result))
	}
}

func TestPlanAllowedFields(t *testing.T) {
	expected := []string{"id", "name", "amount", "currency", "interval", "description"}
	if len(PlanAllowedFields) != len(expected) {
		t.Fatalf("PlanAllowedFields length: got %d, want %d", len(PlanAllowedFields), len(expected))
	}
	for i, f := range expected {
		if PlanAllowedFields[i] != f {
			t.Errorf("PlanAllowedFields[%d] = %q, want %q", i, PlanAllowedFields[i], f)
		}
	}
}

func TestStatementAllowedFields(t *testing.T) {
	expected := []string{"id", "subscription_id", "customer", "period_start", "period_end",
		"issued_at", "total_amount", "currency", "kind", "status"}
	if len(StatementAllowedFields) != len(expected) {
		t.Fatalf("StatementAllowedFields length: got %d, want %d", len(StatementAllowedFields), len(expected))
	}
	for i, f := range expected {
		if StatementAllowedFields[i] != f {
			t.Errorf("StatementAllowedFields[%d] = %q, want %q", i, StatementAllowedFields[i], f)
		}
	}
}
