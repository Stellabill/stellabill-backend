package handlers

import (
	"net/http"

	"stellarbill-backend/internal/config"

	"github.com/gin-gonic/gin"
)

// ConfigDumpHandler returns an HTTP handler that dumps the effective runtime
// configuration with secret fields redacted.
//
// Secret fields tagged with `secret:"true"` in the Config struct are replaced
// with "***REDACTED***" before serialization. This ensures that sensitive
// values (database URLs, JWT secrets, admin tokens, Redis passwords) are
// never exposed through the diagnostics endpoint.
//
// The handler is intended for admin-authenticated or loopback-bound routes
// only.
func ConfigDumpHandler(cfg *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		dump := config.Dump(cfg)
		c.JSON(http.StatusOK, dump)
	}
}
