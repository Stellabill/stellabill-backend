package logger

import (
	"os"

	"github.com/sirupsen/logrus"
)

var Log = logrus.New()

func Init() {
	Log.SetFormatter(&logrus.JSONFormatter{})
	Log.SetOutput(os.Stdout)

	level := os.Getenv("LOG_LEVEL")
	switch level {
	case "debug":
		Log.SetLevel(logrus.DebugLevel)
	case "warn":
		Log.SetLevel(logrus.WarnLevel)
	case "error":
		Log.SetLevel(logrus.ErrorLevel)
	default:
		Log.SetLevel(logrus.InfoLevel)
	}
}