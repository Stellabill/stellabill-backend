package timeutil

import (
	"strings"
	"time"
)

// NowUTC returns the current wall clock time normalized to UTC.
func NowUTC() time.Time {
	return time.Now().UTC()
}

// NormalizeUTC converts a timestamp to UTC while preserving the same instant.
func NormalizeUTC(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	return t.UTC()
}

// NormalizePtrUTC converts a nullable timestamp to UTC.
func NormalizePtrUTC(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	normalized := NormalizeUTC(*t)
	return &normalized
}

// ParseRFC3339ToUTC parses RFC3339 input and normalizes it to UTC.
func ParseRFC3339ToUTC(raw string) (time.Time, error) {
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}

// NormalizeRFC3339StringToUTC parses RFC3339 input and returns RFC3339 UTC output.
func NormalizeRFC3339StringToUTC(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	ts, err := ParseRFC3339ToUTC(raw)
	if err != nil {
		return "", err
	}
	return FormatRFC3339UTC(ts), nil
}

// FormatRFC3339UTC renders a timestamp as an RFC3339 UTC string.
func FormatRFC3339UTC(t time.Time) string {
	return NormalizeUTC(t).Format(time.RFC3339)
}

// FormatRFC3339UTCPtr renders a nullable timestamp as RFC3339 UTC.
func FormatRFC3339UTCPtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	formatted := FormatRFC3339UTC(*t)
	return &formatted
}
