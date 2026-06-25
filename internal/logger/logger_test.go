package logger

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/sirupsen/logrus"
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
