package timeutil

import (
	"testing"
	"time"
)

func TestCoverage_FormatRFC3339UTCPtr(t *testing.T) {
	if FormatRFC3339UTCPtr(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
	now := time.Now()
	got := FormatRFC3339UTCPtr(&now)
	if got == nil || *got == "" {
		t.Fatal("expected formatted output")
	}
}

func TestCoverage_NormalizePtrUTC(t *testing.T) {
	if NormalizePtrUTC(nil) != nil {
		t.Fatal("expected nil")
	}
	now := time.Now()
	got := NormalizePtrUTC(&now)
	if got == nil {
		t.Fatal("expected non-nil")
	}
}

func TestCoverage_NormalizeUTC(t *testing.T) {
	var zero time.Time
	got := NormalizeUTC(zero)
	if !got.IsZero() {
		t.Fatal("expected zero")
	}
}

func TestCoverage_NowUTC(t *testing.T) {
	_ = NowUTC()
}

func TestCoverage_FormatRFC3339UTC(t *testing.T) {
	_ = FormatRFC3339UTC(time.Now())
}

func TestCoverage_ParseRFC3339_Errors(t *testing.T) {
	if _, err := ParseRFC3339ToUTC("not-a-time"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := NormalizeRFC3339StringToUTC("not-a-time"); err == nil {
		t.Fatal("expected error")
	}
	if s, err := NormalizeRFC3339StringToUTC(""); err != nil || s != "" {
		t.Fatalf("expected empty result, got %q err=%v", s, err)
	}
	if s, err := NormalizeRFC3339StringToUTC("2024-01-02T00:00:00Z"); err != nil || s == "" {
		t.Fatalf("expected formatted, got %q err=%v", s, err)
	}
}
