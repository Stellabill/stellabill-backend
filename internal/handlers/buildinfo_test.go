package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestBuildInfo_Returns200 verifies the endpoint returns HTTP 200 and
// all expected fields are present and non-empty.
func TestBuildInfo_Returns200(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/buildinfo", nil)

	h.BuildInfo(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BuildInfoResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	// All fields must be present.
	assert.NotEmpty(t, resp.Version)
	assert.NotEmpty(t, resp.Commit)
	assert.NotEmpty(t, resp.BuildTime)
	assert.NotEmpty(t, resp.GoVersion)
	assert.NotEmpty(t, resp.GoOS)
	assert.NotEmpty(t, resp.GoArch)
}

// TestBuildInfo_RuntimeFieldsCorrect verifies go_version, go_os, go_arch
// match runtime values.
func TestBuildInfo_RuntimeFieldsCorrect(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/buildinfo", nil)

	h.BuildInfo(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BuildInfoResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	assert.Equal(t, runtime.Version(), resp.GoVersion)
	assert.Equal(t, runtime.GOOS, resp.GoOS)
	assert.Equal(t, runtime.GOARCH, resp.GoArch)
}

// TestBuildInfo_DevDefaults ensures that when ldflags are not set the
// endpoint returns sensible defaults ("dev" / "unknown") instead of
// failing.  Because collectBuildInfo enriches the response from VCS
// settings when available (e.g. go test from a git repo), we set the
// globals to non-triggering sentinel values so VCS cannot override them.
func TestBuildInfo_DevDefaults(t *testing.T) {
	origVersion := buildVersion
	origCommit := buildCommit
	origBuildTime := buildTime

	// Use sentinel values that won't trigger VCS override logic.
	buildVersion = "dev-sentinel"
	buildCommit = "unknown-sentinel"
	buildTime = ""

	defer func() {
		buildVersion = origVersion
		buildCommit = origCommit
		buildTime = origBuildTime
	}()

	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/buildinfo", nil)

	h.BuildInfo(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BuildInfoResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	// Sentinel values are preserved (VCS does not override them).
	assert.Equal(t, "dev-sentinel", resp.Version)
	assert.Equal(t, "unknown-sentinel", resp.Commit)
	// BuildTime should be non-empty (zero-timestamp fallback or VCS).
	assert.NotEmpty(t, resp.BuildTime)
}

// TestBuildInfo_LdflagsInjection verifies that when ldflags are set the
// endpoint returns the injected values correctly.
func TestBuildInfo_LdflagsInjection(t *testing.T) {
	origVersion := buildVersion
	origCommit := buildCommit
	origBuildTime := buildTime

	buildVersion = "v1.2.3-beta.4"
	buildCommit = "a1b2c3d4e5f6"
	buildTime = "2025-06-15T14:30:00Z"

	defer func() {
		buildVersion = origVersion
		buildCommit = origCommit
		buildTime = origBuildTime
	}()

	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/buildinfo", nil)

	h.BuildInfo(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp BuildInfoResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	assert.Equal(t, "v1.2.3-beta.4", resp.Version)
	assert.Equal(t, "a1b2c3d4e5f6", resp.Commit)
	assert.Equal(t, "2025-06-15T14:30:00Z", resp.BuildTime)
	assert.Equal(t, runtime.Version(), resp.GoVersion)
}

// TestBuildInfo_NoEnvironmentSecrets verifies the buildinfo response
// does not accidentally leak environment secrets.
func TestBuildInfo_NoEnvironmentSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/buildinfo", nil)

	h.BuildInfo(c)

	body := w.Body.String()

	// Common secret patterns that must never appear.
	assert.NotContains(t, body, "DATABASE_URL")
	assert.NotContains(t, body, "JWT_SECRET")
	assert.NotContains(t, body, "ADMIN_TOKEN")
	assert.NotContains(t, body, "password")
	assert.NotContains(t, body, "secret")
	assert.NotContains(t, body, "token")
	assert.NotContains(t, body, "API_KEY")
}

// TestBuildInfo_ResponseContentType verifies the Content-Type header is
// application/json.
func TestBuildInfo_ResponseContentType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/internal/buildinfo", nil)

	h.BuildInfo(c)

	assert.Equal(t, "application/json; charset=utf-8", w.Header().Get("Content-Type"))
}

// TestCollectBuildInfo_ReturnsNonEmpty ensures collectBuildInfo always
// produces a valid response with no empty critical fields.
func TestCollectBuildInfo_ReturnsNonEmpty(t *testing.T) {
	bi := collectBuildInfo()

	assert.NotEmpty(t, bi.Version)
	assert.NotEmpty(t, bi.Commit)
	assert.NotEmpty(t, bi.BuildTime)
	assert.NotEmpty(t, bi.GoVersion)
	assert.NotEmpty(t, bi.GoOS)
	assert.NotEmpty(t, bi.GoArch)

	// GoOS and GoArch must match runtime.
	assert.Equal(t, runtime.GOOS, bi.GoOS)
	assert.Equal(t, runtime.GOARCH, bi.GoArch)
}

// TestCollectBuildInfo_BuildTimeFallback ensures that when neither ldflags
// nor VCS provide a build time, a zero-value RFC3339 timestamp is returned.
// Use sentinel values for version/commit so VCS enrichment does not
// interfere with the buildTime-only assertion.
func TestCollectBuildInfo_BuildTimeFallback(t *testing.T) {
	origVersion := buildVersion
	origCommit := buildCommit
	origBuildTime := buildTime

	buildVersion = "buildtime-fallback-sentinel"
	buildCommit = "buildtime-fallback-sentinel"
	buildTime = ""

	defer func() {
		buildVersion = origVersion
		buildCommit = origCommit
		buildTime = origBuildTime
	}()

	bi := collectBuildInfo()
	assert.NotEmpty(t, bi.BuildTime)
	// Should be a valid RFC3339 timestamp (even if it's the zero time).
	assert.Contains(t, bi.BuildTime, "T")
	assert.Contains(t, bi.BuildTime, "Z")
}
