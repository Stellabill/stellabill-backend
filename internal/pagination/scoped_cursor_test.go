package pagination

import (
	"testing"
)

func TestScopedCursor_RoundTrip(t *testing.T) {
	encoded := EncodeScopedCursor("id-1", "sort-val", "tenant-abc")
	if encoded == "" {
		t.Fatal("expected non-empty encoded cursor")
	}

	cursor, err := DecodeScopedCursor(encoded, "tenant-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cursor.ID != "id-1" || cursor.SortValue != "sort-val" {
		t.Fatalf("unexpected cursor: %+v", cursor)
	}
}

func TestScopedCursor_EmptyString(t *testing.T) {
	cursor, err := DecodeScopedCursor("", "tenant-abc")
	if err != nil {
		t.Fatalf("unexpected error for empty cursor: %v", err)
	}
	if cursor.ID != "" || cursor.SortValue != "" {
		t.Fatalf("expected empty cursor, got: %+v", cursor)
	}
}

func TestScopedCursor_WrongTenantRejected(t *testing.T) {
	encoded := EncodeScopedCursor("id-1", "sort-val", "tenant-abc")
	_, err := DecodeScopedCursor(encoded, "tenant-xyz")
	if err == nil {
		t.Fatal("expected error for wrong tenant")
	}
}

func TestScopedCursor_TamperedSignatureRejected(t *testing.T) {
	// Garbage base64 that decodes to valid JSON but with wrong sig
	_, err := DecodeScopedCursor("eyJpZCI6ImlkLTEiLCJ0ZW5hbnRfaWQiOiJ0ZW5hbnQtYWJjIiwic2lnIjoiZmFrZSJ9", "tenant-abc")
	if err == nil {
		t.Fatal("expected error for tampered signature")
	}
}

func TestScopedCursor_InvalidBase64(t *testing.T) {
	_, err := DecodeScopedCursor("not-valid-base64!!!", "tenant-abc")
	if err == nil {
		t.Fatal("expected error for invalid base64")
	}
}
