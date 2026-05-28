package pagination

import (
	"os"
	"testing"
)

func TestCoverage_ScopedCursor(t *testing.T) {
	os.Setenv("CURSOR_HMAC_SECRET", "test-secret")
	defer os.Unsetenv("CURSOR_HMAC_SECRET")

	enc := EncodeScopedCursor("id1", "sort1", "tenant1")
	if enc == "" {
		t.Fatal("expected non-empty")
	}
	if EncodeScopedCursor("", "", "tenant1") != "" {
		t.Fatal("expected empty for empty id/sort")
	}

	c, err := DecodeScopedCursor(enc, "tenant1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if c.ID != "id1" {
		t.Fatalf("expected id1, got %s", c.ID)
	}

	_, err = DecodeScopedCursor(enc, "tenant2")
	if err == nil {
		t.Fatal("expected tenant mismatch error")
	}

	if _, err = DecodeScopedCursor("", "tenant1"); err != nil {
		t.Fatalf("unexpected err for empty: %v", err)
	}
	if _, err = DecodeScopedCursor("!!!notbase64!!!", "tenant1"); err == nil {
		t.Fatal("expected invalid format error")
	}

	// Valid base64 but bad JSON
	if _, err = DecodeScopedCursor("aGVsbG8=", "tenant1"); err == nil {
		t.Fatal("expected json unmarshal error")
	}

	// Tampered signature
	bad := EncodeScopedCursor("id1", "sort1", "tenant1")
	if _, err = DecodeScopedCursor(bad+"XX", "tenant1"); err == nil {
		t.Fatal("expected tampered cursor error")
	}
}

func TestCoverage_ScopedCursor_NoSecretEnv(t *testing.T) {
	os.Unsetenv("CURSOR_HMAC_SECRET")
	enc := EncodeScopedCursor("id1", "sort1", "tenant1")
	if enc == "" {
		t.Fatal("expected non-empty")
	}
	c, err := DecodeScopedCursor(enc, "tenant1")
	if err != nil || c.ID != "id1" {
		t.Fatalf("decode failed: %v id=%s", err, c.ID)
	}
}

func TestCoverage_CursorEncodeEmpty(t *testing.T) {
	if Encode(Cursor{}) != "" {
		t.Fatal("expected empty for zero cursor")
	}
	enc := Encode(Cursor{ID: "x", SortValue: "y"})
	if enc == "" {
		t.Fatal("expected non-empty cursor")
	}
	got, err := Decode(enc)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "x" || got.SortValue != "y" {
		t.Fatalf("decode mismatch: %+v", got)
	}
	if c, err := Decode(""); err != nil || c.ID != "" {
		t.Fatalf("empty decode failed: %v", err)
	}
	if _, err := Decode("!!!nope"); err == nil {
		t.Fatal("expected error")
	}
	// Valid base64, bad JSON
	if _, err := Decode("aGVsbG8="); err == nil {
		t.Fatal("expected unmarshal error")
	}
}
