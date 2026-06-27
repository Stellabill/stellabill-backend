package security

import (
	"testing"

	"go.uber.org/zap/zapcore"
)

func TestMaskPII(t *testing.T) {
	if MaskPII("") != "" {
		t.Fatal("empty should stay empty")
	}
	got := MaskPII("customer-123 owes $42.50")
	if got == "customer-123 owes $42.50" {
		t.Fatalf("expected redaction, got %q", got)
	}
	_ = MaskPII("cust-abc")
	_ = MaskPII("nothing here")
}

func TestRedactMap(t *testing.T) {
	if RedactMap(nil) != nil {
		t.Fatal("nil should be returned as-is")
	}
	m := map[string]interface{}{
		"token":    "very-secret",
		"password": "hunter2",
		"name":     "customer-42",
		"count":    7,
	}
	out := RedactMap(m)
	if out["token"] != "***REDACTED***" {
		t.Fatalf("token not redacted: %v", out["token"])
	}
	if out["password"] != "***REDACTED***" {
		t.Fatalf("password not redacted: %v", out["password"])
	}
	if out["count"] != 7 {
		t.Fatalf("non-string preserved: %v", out["count"])
	}
}

func TestProductionLogger(t *testing.T) {
	l := ProductionLogger()
	if l == nil {
		t.Fatal("expected non-nil logger")
	}
	entry := zapcore.Entry{Message: "customer-7"}
	if err := ZapRedactHook(entry); err != nil {
		t.Fatal(err)
	}
}
