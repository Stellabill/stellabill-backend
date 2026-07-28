package handlers

import (
	"net/http"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/gin-gonic/gin"
)

// Build-time variables injected via ldflags.
// Example:
//   go build -ldflags="-X stellarbill-backend/internal/handlers.buildVersion=v1.2.3 \
//       -X stellarbill-backend/internal/handlers.buildCommit=abc123 \
//       -X stellarbill-backend/internal/handlers.buildTime=2025-01-15T10:30:00Z" .
var (
	buildVersion = "dev"
	buildCommit  = "unknown"
	buildTime    = ""
)

// BuildInfoResponse represents the information returned by the build-info endpoint.
type BuildInfoResponse struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
	GoOS      string `json:"go_os"`
	GoArch    string `json:"go_arch"`
}

// collectBuildInfo assembles the build information from ldflags and runtime/debug.
func collectBuildInfo() BuildInfoResponse {
	bi := BuildInfoResponse{
		Version:   buildVersion,
		Commit:    buildCommit,
		BuildTime: buildTime,
		GoVersion: runtime.Version(),
		GoOS:      runtime.GOOS,
		GoArch:    runtime.GOARCH,
	}

	// Enrich commit from VCS info embedded by go build when available.
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if buildCommit == "unknown" {
					bi.Commit = setting.Value
				}
			case "vcs.time":
				if buildTime == "" {
					if t, err := time.Parse(time.RFC3339, setting.Value); err == nil {
						bi.BuildTime = t.UTC().Format(time.RFC3339)
					}
				}
			}
		}
		// Use main module version as fallback when ldflags not supplied.
		if buildVersion == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
			bi.Version = info.Main.Version
		}
	}

	// If buildTime remains empty (neither ldflags nor VCS set it), use a zero
	// timestamp so the field is always present and non-null in JSON.
	if bi.BuildTime == "" {
		bi.BuildTime = time.Time{}.UTC().Format(time.RFC3339)
	}

	return bi
}

// BuildInfo returns build information including version, commit, build time,
// and Go runtime details.  Non-VCS (dev) builds return sensible defaults
// instead of failing, making the endpoint safe for local development.
func (h *Handler) BuildInfo(c *gin.Context) {
	bi := collectBuildInfo()
	c.JSON(http.StatusOK, bi)
}
