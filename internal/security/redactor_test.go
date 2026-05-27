package security

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type captureCore struct {
	zapcore.LevelEnabler
	entries    *[]zapcore.Entry
	fieldsList *[][]zapcore.Field
}

func (c *captureCore) With(fields []zapcore.Field) zapcore.Core {
	return c
}

func (c *captureCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return ce.AddCore(ent, c)
}

func (c *captureCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	*c.entries = append(*c.entries, ent)
	*c.fieldsList = append(*c.fieldsList, fields)
	return nil
}

func (c *captureCore) Sync() error {
	return nil
}

type testWriter struct {
	writeFunc func(p []byte) (n int, err error)
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	return w.writeFunc(p)
}

func (w *testWriter) Sync() error {
	return nil
}

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

func TestRedactError(t *testing.T) {
	err1 := errors.New("failed to process payment for customer_abc123")
	redacted := RedactError(err1)
	assert.NotNil(t, redacted)
	assert.NotContains(t, redacted.Error(), "customer_abc123")
	assert.Contains(t, redacted.Error(), "cust_")

	assert.Nil(t, RedactError(nil))
}

func TestMaskers(t *testing.T) {
	assert.Equal(t, "cust***", maskCustomerID("abc123"))
	assert.Equal(t, "***", maskCustomerID("ab"))
	assert.Equal(t, "$*.**", maskAmount("19.99"))
	assert.Equal(t, "$*.**", maskAmount("0"))
	assert.Equal(t, "sub_***", maskSubscriptionID("sub_xyz"))
	assert.Equal(t, "job_***", maskJobID("job_999"))
}

func TestRedactZapFields(t *testing.T) {
	fields := []zap.Field{
		zap.String("customer", "cust_12345"),
		zap.Int("count", 5),
		zap.Error(errors.New("failed for customer_abc")),
	}
	redacted := RedactZapFields(fields)
	assert.Len(t, redacted, 3)
	assert.Equal(t, "cust***", redacted[0].String)
	assert.Equal(t, int64(5), redacted[1].Integer)
	err := redacted[2].Interface.(error)
	assert.NotContains(t, err.Error(), "customer_abc")
}

// TestRedactingCore_Integration tests the full core redaction pipeline.
func TestRedactingCore_Integration(t *testing.T) {
	var entries []zapcore.Entry
	var fieldsList [][]zapcore.Field

	innerCore := &captureCore{
		LevelEnabler: zap.NewAtomicLevel(),
		entries:      &entries,
		fieldsList:   &fieldsList,
	}

	core := NewRedactingCore(innerCore)

	entry := zapcore.Entry{
		Message: "Error for customer_abc and amount 99.99",
		Level:   zapcore.ErrorLevel,
	}
	fields := []zapcore.Field{
		{Key: "customer", Type: zapcore.StringType, String: "cust_123"},
		{Key: "amount", Type: zapcore.StringType, String: "1999.99"},
		{Key: "count", Type: zapcore.Int64Type, Integer: 42},
	}

	_ = core.Write(entry, fields)

	assert.Len(t, entries, 1)
	assert.NotContains(t, entries[0].Message, "customer_abc")
	assert.Contains(t, entries[0].Message, "cust_***")
	assert.Contains(t, entries[0].Message, "$*.**")

	assert.Len(t, fieldsList, 1)
	redactedFields := fieldsList[0]
	var custVal, amtVal string
	var countVal int64
	for _, f := range redactedFields {
		switch f.Key {
		case "customer":
			custVal = f.String
		case "amount":
			amtVal = f.String
		case "count":
			countVal = f.Integer
		}
	}
	assert.Equal(t, "cust***", custVal)
	assert.Equal(t, "$*.**", amtVal)
	assert.Equal(t, int64(42), countVal)
}
