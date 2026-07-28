package logger

import (
	"github.com/sirupsen/logrus"
)

// Log is the package-level logrus instance shared by callers that want a
// pre-configured JSON logger. Helpers were intentionally trimmed because no
// runtime code paths exercise them today.
var Log = logrus.New()

// SafePrintf writes to the shared logger without panicking when the logger is nil.
func SafePrintf(format string, args ...interface{}) {
	if Log != nil {
		Log.Printf(format, args...)
	}
}
