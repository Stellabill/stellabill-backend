package logger

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

// Log is the package-level logrus instance shared by callers that want a
// pre-configured JSON logger. Helpers were intentionally trimmed because no
// runtime code paths exercise them today.
var Log = logrus.New()

// SafePrintf is a convenience wrapper that logs a formatted message at info
// level. It is safe to call with a nil logger (no-op).
func SafePrintf(format string, args ...interface{}) {
	if Log != nil {
		Log.Info(fmt.Sprintf(format, args...))
	}
}
