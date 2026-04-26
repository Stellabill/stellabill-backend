package maintenance

import (
	"sync/atomic"
)

var isMaintenanceMode atomic.Bool

// IsActive returns true if maintenance mode is active.
func IsActive() bool {
	return isMaintenanceMode.Load()
}

// Enable turns on maintenance mode.
func Enable() {
	isMaintenanceMode.Store(true)
}

// Disable turns off maintenance mode.
func Disable() {
	isMaintenanceMode.Store(false)
}
