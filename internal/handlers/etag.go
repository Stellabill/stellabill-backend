package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GenerateETag creates a weak ETag based on timestamp and version.
func GenerateETag(updatedAt time.Time, version int64) string {
	return fmt.Sprintf(`W/"%d-%d"`, updatedAt.UnixNano(), version)
}

// ParseIfMatch extracts the version from an If-Match header.
func ParseIfMatch(header string) (int64, error) {
	if header == "" {
		return 0, fmt.Errorf("missing If-Match header")
	}

	header = strings.Trim(header, `"`)
	if strings.HasPrefix(header, "W/") {
		header = strings.TrimPrefix(header, "W/")
		header = strings.Trim(header, `"`)
	}

	parts := strings.Split(header, "-")
	var versionStr string
	if len(parts) == 2 {
		versionStr = parts[1]
	} else {
		versionStr = parts[0]
	}

	return strconv.ParseInt(versionStr, 10, 64)
}

// EnsureIfMatch checks the If-Match header and returns the expected version.
// If absent or malformed, it sets the appropriate HTTP response and returns an error.
func EnsureIfMatch(c *gin.Context) (int64, error) {
	ifMatch := c.GetHeader("If-Match")
	if ifMatch == "" {
		c.JSON(http.StatusPreconditionRequired, gin.H{"error": "If-Match header is required"})
		return 0, fmt.Errorf("precondition required")
	}

	expectedVersion, err := ParseIfMatch(ifMatch)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid If-Match header format"})
		return 0, err
	}

	return expectedVersion, nil
}
