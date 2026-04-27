package timeutil

import (
	"testing"
	"time"
)

func TestParseRFC3339ToUTC_NormalizesOffset(t *testing.T) {
	got, err := ParseRFC3339ToUTC("2026-04-23T10:30:00+02:00")
	if err != nil {
		t.Fatalf("ParseRFC3339ToUTC returned error: %v", err)
	}

	want := time.Date(2026, 4, 23, 8, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("unexpected instant: got %s want %s", got, want)
	}
	if got.Location() != time.UTC {
		t.Fatalf("expected UTC location, got %v", got.Location())
	}
}

func TestNormalizeRFC3339StringToUTC_EmptyInput(t *testing.T) {
	got, err := NormalizeRFC3339StringToUTC("   ")
	if err != nil {
		t.Fatalf("NormalizeRFC3339StringToUTC returned error: %v", err)
	}
	if got != "" {
		t.Fatalf("expected empty output, got %q", got)
	}
}

func TestNormalizePtrUTC_NilInput(t *testing.T) {
	if NormalizePtrUTC(nil) != nil {
		t.Fatal("expected nil output")
	}
}

func TestFormatRFC3339UTC(t *testing.T) {
	ts := time.Date(2026, 4, 23, 10, 0, 0, 0, time.FixedZone("CET", 3600))
	got := FormatRFC3339UTC(ts)
	if got != "2026-04-23T09:00:00Z" {
		t.Fatalf("unexpected formatted timestamp: %s", got)
	}
}
