package requestparams

import (
	"strings"
	"testing"
)

func TestParseRSQL_AllowsSafeIndexedFields(t *testing.T) {
	filter, err := ParseRSQL(`amount=gt=100;status=in=(open,paid)`)
	if err != nil {
		t.Fatalf("expected parser to accept safe filter, got error: %v", err)
	}
	if filter == nil {
		t.Fatal("expected filter to be returned")
	}
	if got := filter.Fingerprint(); got == "" {
		t.Fatal("expected fingerprint to be generated")
	}
}

func TestParseRSQL_RejectsUnknownOperator(t *testing.T) {
	_, err := ParseRSQL(`amount=foo=100`)
	if err == nil {
		t.Fatal("expected unknown operator error")
	}
	if !strings.Contains(err.Error(), "unsupported operator") {
		t.Fatalf("expected unsupported operator error, got %v", err)
	}
}

func TestParseRSQL_RejectsDisallowedField(t *testing.T) {
	_, err := ParseRSQL(`email==a@example.com`)
	if err == nil {
		t.Fatal("expected disallowed field error")
	}
	if !strings.Contains(err.Error(), "unsupported field") {
		t.Fatalf("expected unsupported field error, got %v", err)
	}
}

func TestParseRSQL_SupportsNestedGroupsAndEscapes(t *testing.T) {
	filter, err := ParseRSQL(`((status=in=(open\,paid,closed),kind==invoice);amount=gt=100)`)
	if err != nil {
		t.Fatalf("expected nested groups and escapes to parse, got error: %v", err)
	}
	if filter == nil {
		t.Fatal("expected filter to be returned")
	}
	if _, err := filter.ToSquirrel(); err != nil {
		t.Fatalf("expected squirrel compiler to succeed, got error: %v", err)
	}
}
