package reconciliation

import (
	"testing"
	"time"
)

func TestCompareMatched(t *testing.T) {
	now := time.Date(2025, 1, 2, 15, 4, 5, 0, time.UTC)
	r := New()
	r.Clock = func() time.Time { return now }

	backend := BackendSubscription{
		SubscriptionID: "sub-1",
		Status:         "active",
		Amount:         1000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{"due": 0},
		UpdatedAt:      now,
	}
	contract := Snapshot{
		SubscriptionID: "sub-1",
		Status:         "active",
		Amount:         1000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{"due": 0},
		ExportedAt:     now,
	}

	rep := r.Compare(backend, &contract)
	if !rep.Matched {
		t.Fatalf("expected match, got mismatches: %#v", rep.Mismatches)
	}
}

func TestCompareMismatches(t *testing.T) {
	now := time.Now().UTC()
	r := New()
	r.Clock = func() time.Time { return now }

	backend := BackendSubscription{
		SubscriptionID: "sub-2",
		Status:         "active",
		Amount:         1500,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{"due": 100},
		UpdatedAt:      now,
	}
	contract := Snapshot{
		SubscriptionID: "sub-2",
		Status:         "cancelled",
		Amount:         1500,
		Currency:       "USD",
		Interval:       "yearly",
		Balances:       map[string]int64{"due": 0, "credit": 50},
		ExportedAt:     now,
	}

	rep := r.Compare(backend, &contract)
	if rep.Matched {
		t.Fatalf("expected mismatches but got match")
	}
	// Expect at least status, interval, balances.due, balances.credit
	wantFields := map[string]bool{"status": true, "interval": true, "balances.due": true, "balances.credit": true}
	for _, m := range rep.Mismatches {
		delete(wantFields, m.Field)
	}
	if len(wantFields) != 0 {
		t.Fatalf("missing expected mismatch fields: %#v", wantFields)
	}
}

func TestCompareMissingSnapshot(t *testing.T) {
	now := time.Now()
	r := New()
	r.Clock = func() time.Time { return now }

	backend := BackendSubscription{
		SubscriptionID: "sub-3",
		Status:         "active",
		Amount:         2000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{},
		UpdatedAt:      now,
	}

	rep := r.Compare(backend, nil)
	if rep.Matched {
		t.Fatalf("expected mismatch due to missing snapshot")
	}
	if len(rep.Mismatches) == 0 || rep.Mismatches[0].Field != "contract_snapshot" {
		t.Fatalf("expected contract_snapshot mismatch, got: %#v", rep.Mismatches)
	}
}

func TestCompareStaleSnapshot(t *testing.T) {
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	r := New()
	r.Clock = func() time.Time { return now }

	backend := BackendSubscription{
		SubscriptionID: "sub-4",
		Status:         "active",
		Amount:         3000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{},
		UpdatedAt:      now,
	}
	// contract exported 48 hours before backend updated -> stale
	contract := Snapshot{
		SubscriptionID: "sub-4",
		Status:         "active",
		Amount:         3000,
		Currency:       "USD",
		Interval:       "monthly",
		Balances:       map[string]int64{},
		ExportedAt:     now.Add(-48 * time.Hour),
	}

	rep := r.Compare(backend, &contract)
	if rep.Matched {
		t.Fatalf("expected stale snapshot to be flagged as mismatch")
	}
	found := false
	for _, m := range rep.Mismatches {
		if m.Field == "snapshot_stale" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("snapshot_stale mismatch not found: %#v", rep.Mismatches)
	}
}
