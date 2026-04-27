package openapi

import "testing"

func TestLoad(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if doc.Paths == nil || doc.Paths.Len() == 0 {
		t.Fatalf("expected non-empty paths")
	}
	if doc.Paths.Find("/api/health") == nil {
		t.Fatalf("expected /api/health to exist")
	}
	if doc.Paths.Find("/api/subscriptions/{id}") == nil {
		t.Fatalf("expected /api/subscriptions/{id} to exist")
	}
}

func TestRawYAML_NotEmpty(t *testing.T) {
	if len(RawYAML()) == 0 {
		t.Fatalf("expected embedded spec to be non-empty")
	}
}

func TestLoadFromData_InvalidYAML(t *testing.T) {
	if _, err := loadFromData([]byte("openapi: [")); err == nil {
		t.Fatalf("expected error for invalid YAML/OpenAPI")
	}
}

func TestLoadFromData_InvalidOpenAPI(t *testing.T) {
	invalid := []byte("openapi: 3.0.3\ninfo: {}\npaths: {}\n")
	if _, err := loadFromData(invalid); err == nil {
		t.Fatalf("expected validation error for invalid OpenAPI document")
	}
}

