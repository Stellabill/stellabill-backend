package logger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"stellarbill-backend/internal/security"
)

func TestLoggerOutputsJSON(t *testing.T) {

	var buf bytes.Buffer
	Log.SetOutput(&buf)
	Log.SetFormatter(&logrus.JSONFormatter{})

	Log.Info("test message")

	var result map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &result)

	if err != nil {
		t.Errorf("log is not valid JSON: %v", err)
	}

	if result["msg"] != "test message" {
		t.Errorf("message field missing, got: %+v", result)
	}
}

func TestSafeInfofRedactsPII(t *testing.T) {
	var buf bytes.Buffer
	Log.SetOutput(&buf)
	Log.SetFormatter(&logrus.JSONFormatter{})

	SafeInfof("Processing customer_abc123 amount 100.50")

	var result map[string]interface{}
	bufBytes := buf.Bytes()
	// There may be multiple log lines; take last
	lines := bytes.Split(bufBytes, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		if len(bytes.TrimSpace(lines[i])) > 0 {
			_ = json.Unmarshal(lines[i], &result)
			break
		}
	}

	msg, ok := result["msg"].(string)
	if !ok {
		t.Fatalf("no msg field in log: %+v", result)
	}
	// Ensure PII masked
	assertFalse(t, strings.Contains(msg, "customer_abc123"))
	assertTrue(t, strings.Contains(msg, "cust_***"))
	assertFalse(t, strings.Contains(msg, "100.50"))
	assertTrue(t, strings.Contains(msg, "$*.**"))
}

func TestRedactMapStringFields_Hook(t *testing.T) {
	// Test that the LogrusHook correctly redacts fields
	var buf bytes.Buffer
	Log.SetOutput(&buf)
	Log.SetFormatter(&logrus.JSONFormatter{})

	entry := Log.WithFields(logrus.Fields{
		"customer": "cust_xyz123",
		"amount":   "999.99",
		"nonpii":   "hello",
	})
	entry.Info("test entry")

	var result map[string]interface{}
	bufBytes := buf.Bytes()
	lines := bytes.Split(bufBytes, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		if len(bytes.TrimSpace(lines[i])) > 0 {
			_ = json.Unmarshal(lines[i], &result)
			break
		}
	}

	customerVal := result["customer"].(string)
	amountVal := result["amount"].(string)
	nonpiiVal := result["nonpii"].(string)

	assertEqual(t, "cust***", customerVal)
	assertEqual(t, "$*.**", amountVal)
	assertEqual(t, "hello", nonpiiVal) // non-PII field should have original? But might have been scanned. It could be same since no pattern.
}

// Helper assertion shortcuts
func assertTrue(t *testing.T, cond bool) {
	if !cond {
		t.Fatal("expected true")
	}
}
func assertFalse(t *testing.T, cond bool) {
	if cond {
		t.Fatal("expected false")
	}
}
func assertEqual(t *testing.T, expected, actual interface{}) {
	if expected != actual {
		t.Fatalf("expected %v, got %v", expected, actual)
	}
}
