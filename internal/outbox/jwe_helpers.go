package outbox

import (
	"time"

	"github.com/google/uuid"
)

func timeNow() time.Time {
	return time.Now()
}

func newEventDataID() string {
	return uuid.New().String()
}
