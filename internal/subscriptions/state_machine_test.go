package subscriptions

import (
	"testing"
)

func TestCanTransition_ExplicitMatrix(t *testing.T) {
	// Hardcoded transition matrix: each entry specifies the exact expected
	// outcome so that a mutant that flips a guard is always caught.
	tests := []struct {
		from    string
		to      string
		wantErr bool
		errMsg  string
	}{
		// ---- same-state (no-op) ----
		{StatusPending, StatusPending, false, ""},
		{StatusActive, StatusActive, false, ""},
		{StatusPaused, StatusPaused, false, ""},
		{StatusCancelled, StatusCancelled, false, ""},
		{StatusExpired, StatusExpired, false, ""},

		// ---- pending ----
		{StatusPending, StatusActive, false, ""},
		{StatusPending, StatusCancelled, false, ""},
		{StatusPending, StatusPaused, true, "invalid transition from pending to paused"},
		{StatusPending, StatusExpired, true, "invalid transition from pending to expired"},

		// ---- active ----
		{StatusActive, StatusPaused, false, ""},
		{StatusActive, StatusCancelled, false, ""},
		{StatusActive, StatusExpired, false, ""},
		{StatusActive, StatusPending, true, "invalid transition from active to pending"},

		// ---- paused ----
		{StatusPaused, StatusActive, false, ""},
		{StatusPaused, StatusCancelled, false, ""},
		{StatusPaused, StatusPending, true, "invalid transition from paused to pending"},
		{StatusPaused, StatusExpired, true, "invalid transition from paused to expired"},

		// ---- cancelled (terminal) ----
		{StatusCancelled, StatusActive, true, "invalid transition from cancelled to active"},
		{StatusCancelled, StatusPaused, true, "invalid transition from cancelled to paused"},
		{StatusCancelled, StatusPending, true, "invalid transition from cancelled to pending"},
		{StatusCancelled, StatusExpired, true, "invalid transition from cancelled to expired"},

		// ---- expired (terminal) ----
		{StatusExpired, StatusActive, true, "invalid transition from expired to active"},
		{StatusExpired, StatusPaused, true, "invalid transition from expired to paused"},
		{StatusExpired, StatusPending, true, "invalid transition from expired to pending"},
		{StatusExpired, StatusCancelled, true, "invalid transition from expired to cancelled"},

		// ---- unknown source ----
		{"unknown_state", StatusActive, true, "unknown current state: unknown_state"},
		{"unknown_state", StatusCancelled, true, "unknown current state: unknown_state"},
		{"unknown_state", "unknown_state", true, "unknown current state: unknown_state"},

		// ---- unknown target ----
		{StatusPending, "bogus", true, "invalid transition from pending to bogus"},
		{StatusActive, "bogus", true, "invalid transition from active to bogus"},
	}

	for _, tc := range tests {
		t.Run(tc.from+"_to_"+tc.to, func(t *testing.T) {
			err := CanTransition(tc.from, tc.to)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error %q but got nil", tc.errMsg)
				}
				if err.Error() != tc.errMsg {
					t.Fatalf("expected error %q, got %q", tc.errMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestCanTransition_UnknownSource(t *testing.T) {
	err := CanTransition("mystery", StatusActive)
	if err == nil {
		t.Fatal("expected error for unknown source state")
	}
	want := "unknown current state: mystery"
	if err.Error() != want {
		t.Fatalf("got %q, want %q", err.Error(), want)
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
