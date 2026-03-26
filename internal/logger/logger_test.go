package logger

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLoggerOutputsJSON(t *testing.T) {

	var buf bytes.Buffer
	Log.SetOutput(&buf)

	Init()

	Log.Info("test message")

	var result map[string]interface{}
	err := json.Unmarshal(buf.Bytes(), &result)

	if err != nil {
		t.Errorf("log is not valid JSON")
	}

	if result["msg"] != "test message" {
		t.Errorf("message field missing")
	}
}