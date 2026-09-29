package handlers

import (
	"net/http"
	"runtime"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

type BuildInfoResponse struct {
	Version      string            `json:"version"`
	Commit       string            `json:"commit"`
	BuildTime    string            `json:"build_time"`
	GoVersion    string            `json:"go_version"`
	Compiler     string            `json:"compiler"`
	Architecture string            `json:"architecture"`
	OS           string            `json:"os"`
	Settings     map[string]string `json:"settings,omitempty"`
}

func GetBuildInfo(c *gin.Context) {
	version := "dev"
	commit := "unknown"
	buildTime := "unknown"
	settingsMap := make(map[string]string)

	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				commit = setting.Value
			case "vcs.time":
				buildTime = setting.Value
			case "vcs.modified":
				settingsMap[setting.Key] = setting.Value
			}
		}
	}

	c.JSON(http.StatusOK, BuildInfoResponse{
		Version:      version,
		Commit:       commit,
		BuildTime:    buildTime,
		GoVersion:    runtime.Version(),
		Compiler:     runtime.Compiler,
		Architecture: runtime.GOARCH,
		OS:           runtime.GOOS,
		Settings:     settingsMap,
	})
}
