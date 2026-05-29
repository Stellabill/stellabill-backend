package pagination

import (
	"errors"
	"strconv"
	"strings"
)

const (
	// DefaultLimit is the fallback limit when no limit is provided or it is <= 0.
	DefaultLimit = 20
	// MaxLimit is the maximum allowed pagination page size.
	MaxLimit = 100
)

// ErrInvalidLimit is returned when a limit parameter is not a valid integer.
var ErrInvalidLimit = errors.New("invalid limit value")

// ParseLimit parses the raw limit query parameter.
// It enforces the following rules:
// - empty string (missing or empty) -> defaultLimit
// - valid integer <= 0 -> defaultLimit
// - valid integer > MaxLimit -> MaxLimit
// - invalid integer (non-numeric, float, overflow) -> ErrInvalidLimit
// - whitespace-only -> ErrInvalidLimit
func ParseLimit(raw string, defaultLimit int) (int, error) {
	if raw == "" {
		return defaultLimit, nil
	}

	// Reject whitespace-only values
	if strings.TrimSpace(raw) == "" {
		return 0, ErrInvalidLimit
	}

	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, ErrInvalidLimit
	}

	if limit <= 0 {
		return defaultLimit, nil
	}

	if limit > MaxLimit {
		return MaxLimit, nil
	}

	return limit, nil
}
