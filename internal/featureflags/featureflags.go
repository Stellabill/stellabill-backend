package featureflags

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const FaultInjectionEnabledFlag = "fault_injection_enabled"

type Flag struct {
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
	Version     int64     `json:"version"`
}

type Manager struct {
	flags map[string]*Flag
	db    map[string]bool // NEW: DB layer
	mutex sync.RWMutex
}

// NewManager returns a fresh, isolated feature flag manager instance.
func NewManager() *Manager {
	return &Manager{
		flags: make(map[string]*Flag),
		db:    make(map[string]bool),
	}
}

// Set sets a flag value.
func (m *Manager) Set(name string, enabled bool, description string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.flags[name] = &Flag{
		Name:        name,
		Enabled:     enabled,
		Description: description,
		UpdatedAt:   time.Now(),
		Version:     1,
	}
}

// Get returns a flag value.
func (m *Manager) Get(name string) (bool, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	f, ok := m.flags[name]
	if !ok {
		return false, false
	}
	return f.Enabled, true
}

// IsEnabled returns true if the flag is enabled.
func (m *Manager) IsEnabled(name string) bool {
	enabled, ok := m.Get(name)
	return ok && enabled
}

// List returns all flags.
func (m *Manager) List() []Flag {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	flags := make([]Flag, 0, len(m.flags))
	for _, f := range m.flags {
		flags = append(flags, *f)
	}
	return flags
}

// MarshalJSON implements json.Marshaler.
func (m *Manager) MarshalJSON() ([]byte, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return json.Marshal(m.flags)
}

// UnmarshalJSON implements json.Unmarshaler.
func (m *Manager) UnmarshalJSON(data []byte) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return json.Unmarshal(data, &m.flags)
}

// SetDB sets a flag in the DB layer.
func (m *Manager) SetDB(name string, enabled bool) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.db[name] = enabled
}

// GetDB gets a flag from the DB layer.
func (m *Manager) GetDB(name string) (bool, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	enabled, ok := m.db[name]
	return enabled, ok
}
