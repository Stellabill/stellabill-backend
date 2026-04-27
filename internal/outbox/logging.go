package outbox

import (
	"os"
	"time"

	"stellabill-backend/internal/structuredlog"
)

var defaultLogger = structuredlog.New(os.Stdout)

func outboxFields(route string, status any) structuredlog.Fields {
	return structuredlog.Fields{
		structuredlog.FieldRequestID: "",
		structuredlog.FieldActor:     "system",
		structuredlog.FieldTenant:    "system",
		structuredlog.FieldRoute:     route,
		structuredlog.FieldStatus:    status,
		structuredlog.FieldDuration:  int64(0),
	}
}

func addDuration(fields structuredlog.Fields, started time.Time) structuredlog.Fields {
	fields[structuredlog.FieldDuration] = time.Since(started).Milliseconds()
	return fields
}
