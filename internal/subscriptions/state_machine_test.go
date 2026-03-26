package subscriptions

import "testing"

func TestValidTransitions(t *testing.T) {
	cases := []struct {
		from string
		to   string
	}{
		{StatusPending, StatusActive},
		{StatusPending, StatusCancelled},
		{StatusActive, StatusPaused},
		{StatusPaused, StatusActive},
	}

	for _, c := range cases {
		if err := CanTransition(c.from, c.to); err != nil {
			t.Errorf("expected valid transition %s -> %s, got error: %v", c.from, c.to, err)
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	cases := []struct {
		from string
		to   string
	}{
		{StatusCancelled, StatusActive},
		{StatusExpired, StatusActive},
		{StatusPending, StatusExpired},
	}

	for _, c := range cases {
		if err := CanTransition(c.from, c.to); err == nil {
			t.Errorf("expected invalid transition %s -> %s", c.from, c.to)
		}
	}
}

func TestNoOpTransition(t *testing.T) {
	if err := CanTransition(StatusActive, StatusActive); err != nil {
		t.Errorf("expected no-op transition to pass")
	}
}
