package pagination

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		defaultLimit int
		expectedVal  int
		expectedErr  error
	}{
		{
			name:         "valid limit within bounds - 10",
			raw:          "10",
			defaultLimit: 20,
			expectedVal:  10,
			expectedErr:  nil,
		},
		{
			name:         "valid limit within bounds - 1",
			raw:          "1",
			defaultLimit: 20,
			expectedVal:  1,
			expectedErr:  nil,
		},
		{
			name:         "valid limit - default value - 20",
			raw:          "20",
			defaultLimit: 20,
			expectedVal:  20,
			expectedErr:  nil,
		},
		{
			name:         "valid limit - max boundary 100",
			raw:          "100",
			defaultLimit: 20,
			expectedVal:  100,
			expectedErr:  nil,
		},
		{
			name:         "above max - clamp to 100 (101)",
			raw:          "101",
			defaultLimit: 20,
			expectedVal:  100,
			expectedErr:  nil,
		},
		{
			name:         "extremely large value - clamp to 100 (100000)",
			raw:          "100000",
			defaultLimit: 20,
			expectedVal:  100,
			expectedErr:  nil,
		},
		{
			name:         "zero value - fall back to default limit",
			raw:          "0",
			defaultLimit: 20,
			expectedVal:  20,
			expectedErr:  nil,
		},
		{
			name:         "negative value - fall back to default limit",
			raw:          "-1",
			defaultLimit: 20,
			expectedVal:  20,
			expectedErr:  nil,
		},
		{
			name:         "extremely negative value - fall back to default limit",
			raw:          "-100",
			defaultLimit: 20,
			expectedVal:  20,
			expectedErr:  nil,
		},
		{
			name:         "empty value - fall back to default limit",
			raw:          "",
			defaultLimit: 20,
			expectedVal:  20,
			expectedErr:  nil,
		},
		{
			name:         "malformed value - non-numeric (abc)",
			raw:          "abc",
			defaultLimit: 20,
			expectedVal:  0,
			expectedErr:  ErrInvalidLimit,
		},
		{
			name:         "malformed value - partial non-numeric (1abc)",
			raw:          "1abc",
			defaultLimit: 20,
			expectedVal:  0,
			expectedErr:  ErrInvalidLimit,
		},
		{
			name:         "whitespace values - space only",
			raw:          " ",
			defaultLimit: 20,
			expectedVal:  0,
			expectedErr:  ErrInvalidLimit,
		},
		{
			name:         "whitespace values - tab only",
			raw:          "\t",
			defaultLimit: 20,
			expectedVal:  0,
			expectedErr:  ErrInvalidLimit,
		},
		{
			name:         "overflow values - int64 overflow",
			raw:          "9223372036854775808", // MaxInt64 + 1
			defaultLimit: 20,
			expectedVal:  0,
			expectedErr:  ErrInvalidLimit,
		},
		{
			name:         "overflow values - extremely long digits",
			raw:          "123456789012345678901234567890",
			defaultLimit: 20,
			expectedVal:  0,
			expectedErr:  ErrInvalidLimit,
		},
		{
			name:         "leading whitespace malformed",
			raw:          " 10",
			defaultLimit: 20,
			expectedVal:  0,
			expectedErr:  ErrInvalidLimit,
		},
		{
			name:         "trailing whitespace malformed",
			raw:          "10 ",
			defaultLimit: 20,
			expectedVal:  0,
			expectedErr:  ErrInvalidLimit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := ParseLimit(tt.raw, tt.defaultLimit)
			if tt.expectedErr != nil {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectedErr))
				assert.Equal(t, tt.expectedVal, val)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedVal, val)
			}
		})
	}
}
