package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestGenerateETag(t *testing.T) {
	updatedAt := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	version := int64(42)

	etag := GenerateETag(updatedAt, version)
	expected := `W/"1672574400000000000-42"`
	if etag != expected {
		t.Errorf("Expected %q, got %q", expected, etag)
	}
}

func TestParseIfMatch(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		want    int64
		wantErr bool
	}{
		{"empty", "", 0, true},
		{"valid weak format", `W/"123-42"`, 42, false},
		{"valid weak format with quotes", `"W/\"123-42\""`, 42, false},
		{"no timestamp, just version", `"42"`, 42, false},
		{"invalid format", "abc", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseIfMatch(tt.header)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseIfMatch() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseIfMatch() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureIfMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		headerValue    string
		expectedStatus int
		expectedVer    int64
		expectErr      bool
	}{
		{"missing header", "", http.StatusPreconditionRequired, 0, true},
		{"invalid header", "abc", http.StatusBadRequest, 0, true},
		{"valid header", `W/"123-42"`, http.StatusOK, 42, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("PATCH", "/", nil)
			if tt.headerValue != "" {
				c.Request.Header.Set("If-Match", tt.headerValue)
			}

			ver, err := EnsureIfMatch(c)
			if (err != nil) != tt.expectErr {
				t.Errorf("EnsureIfMatch() error = %v, wantErr %v", err, tt.expectErr)
			}
			if ver != tt.expectedVer {
				t.Errorf("EnsureIfMatch() got = %v, want %v", ver, tt.expectedVer)
			}
			if tt.expectErr && w.Code != tt.expectedStatus {
				t.Errorf("Expected HTTP status %d, got %d", tt.expectedStatus, w.Code)
			}
		})
	}
}
