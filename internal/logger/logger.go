package logger

import (
	"github.com/sirupsen/logrus"
)

// Log is the package-level logrus instance shared by callers that want a
// pre-configured JSON logger. Helpers were intentionally trimmed because no
// runtime code paths exercise them today.
var Log = logrus.New()

// SafePrintf is a safe wrapper around fmt.Sprintf that logs to the package
// logger at info level. It's used by middleware that needs to avoid direct
// stdout logging.
func SafePrintf(format string, args ...interface{}) {
	Log.Infof(format, args...)
}
