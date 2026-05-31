package subscriptions

import (
	"fmt"
	"testing"
)

func TestCanTransition_Exhaustive(t *testing.T) {
	states := []string{
		StatusPending,
		StatusActive,
		StatusPaused,
		StatusCancelled,
		StatusExpired,
	}
	unknownState := "unknown_state"

	type testCase struct {
		from    string
		to      string
		wantErr bool
		errStr  string
	}

	var cases []testCase

	// Known to Known
	for _, from := range states {
		for _, to := range states {
			err := CanTransition(from, to)
			tc := testCase{from: from, to: to}
			if err != nil {
				tc.wantErr = true
				tc.errStr = err.Error()
			}
			cases = append(cases, tc)
		}
	}

	// Unknown to Known
	for _, to := range states {
		cases = append(cases, testCase{
			from:    unknownState,
			to:      to,
			wantErr: true,
			errStr:  fmt.Sprintf("unknown current state: %s", unknownState),
		})
	}

	// Known to Unknown
	for _, from := range states {
		cases = append(cases, testCase{
			from:    from,
			to:      unknownState,
			wantErr: true,
			errStr:  fmt.Sprintf("invalid transition from %s to %s", from, unknownState),
		})
	}

	// Unknown to Unknown
	cases = append(cases, testCase{
		from:    unknownState,
		to:      unknownState,
		wantErr: true,
		errStr:  fmt.Sprintf("unknown current state: %s", unknownState),
	})

	for _, tc := range cases {
		t.Run(fmt.Sprintf("from_%s_to_%s", tc.from, tc.to), func(t *testing.T) {
			err := CanTransition(tc.from, tc.to)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
				if err.Error() != tc.errStr {
					t.Fatalf("expected error %q, got %q", tc.errStr, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestIsKnownStatus(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{StatusPending, true},
		{StatusActive, true},
		{StatusPaused, true},
		{StatusCancelled, true},
		{StatusExpired, true},
		{"unknown", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsKnownStatus(tt.status); got != tt.want {
			t.Errorf("IsKnownStatus(%q) = %v, want %v", tt.status, got, tt.want)
		}
	}
}
