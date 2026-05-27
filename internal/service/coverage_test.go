package service

import "testing"

func TestCoverage_SubscriptionDetail_MarshalJSON(t *testing.T) {
	sd := &SubscriptionDetail{ID: "s1", Customer: "cust_abc"}
	b, err := sd.MarshalJSON()
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("expected non-empty output")
	}
}
