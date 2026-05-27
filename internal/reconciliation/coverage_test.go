package reconciliation

import (
	"context"
	"testing"
)

func TestCoverage_ReportGetters(t *testing.T) {
	r := Report{SubscriptionID: "s1"}
	if r.GetID() != "s1" {
		t.Fatal("GetID mismatch")
	}
	_ = r.GetSortValue()
}

func TestCoverage_MemoryAdapter(t *testing.T) {
	a := NewMemoryAdapter(Snapshot{SubscriptionID: "s1"}, Snapshot{SubscriptionID: "s2"})
	got, err := a.FetchSnapshots(context.Background())
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(got))
	}
}

func TestCoverage_MemoryStore(t *testing.T) {
	s := NewMemoryStore()
	if err := s.SaveReports([]Report{{SubscriptionID: "s1", TenantID: "t1"}, {SubscriptionID: "s2", TenantID: "t2"}}); err != nil {
		t.Fatal(err)
	}
	all, err := s.ListReports()
	if err != nil || len(all) != 2 {
		t.Fatalf("ListReports failed: %v len=%d", err, len(all))
	}
	tn, err := s.ListReportsByTenant("t1")
	if err != nil || len(tn) != 1 {
		t.Fatalf("ListReportsByTenant failed: %v len=%d", err, len(tn))
	}
	tn2, err := s.ListReportsByTenant("missing")
	if err != nil || len(tn2) != 0 {
		t.Fatalf("ListReportsByTenant missing failed: %v len=%d", err, len(tn2))
	}
}
