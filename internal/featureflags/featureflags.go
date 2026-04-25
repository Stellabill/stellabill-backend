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

type Flag struct {
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Manager struct {
	flags map[string]*Flag
	mutex sync.RWMutex
}

var (
	instance *Manager
	once     sync.Once
)

func GetInstance() *Manager {
	once.Do(func() {
		instance = &Manager{
			flags: make(map[string]*Flag),
		}
		instance.loadDefaultFlags()
		instance.loadFromEnvironment()
	})
	return instance
}

func (m *Manager) loadDefaultFlags() {
	defaultFlags := map[string]*Flag{
		"subscriptions_enabled": {
			Name:        "subscriptions_enabled",
			Enabled:     true,
			Description: "Enable subscription management endpoints",
			UpdatedAt:   time.Now(),
		},
		"plans_enabled": {
			Name:        "plans_enabled",
			Enabled:     true,
			Description: "Enable billing plans endpoints",
			UpdatedAt:   time.Now(),
		},
		"new_billing_flow": {
			Name:        "new_billing_flow",
			Enabled:     false,
			Description: "Enable new billing flow feature",
			UpdatedAt:   time.Now(),
		},
		"advanced_analytics": {
			Name:        "advanced_analytics",
			Enabled:     false,
			Description: "Enable advanced analytics endpoints",
			UpdatedAt:   time.Now(),
		},
	}

	for name, flag := range defaultFlags {
		m.flags[name] = flag
	}
}

func (m *Manager) loadFromEnvironment() {
	if flagsJSON := os.Getenv("FEATURE_FLAGS"); flagsJSON != "" {
		var envFlags map[string]bool
		if err := json.Unmarshal([]byte(flagsJSON), &envFlags); err == nil {
			m.mutex.Lock()
			defer m.mutex.Unlock()

			for name, enabled := range envFlags {
				if flag, exists := m.flags[name]; exists {
					flag.Enabled = enabled
					flag.UpdatedAt = time.Now()
				} else {
					m.flags[name] = &Flag{
						Name:        name,
						Enabled:     enabled,
						Description: "Environment-defined flag",
						UpdatedAt:   time.Now(),
					}
				}
			}
		}
	}

	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "FF_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				flagName := strings.ToLower(strings.TrimPrefix(parts[0], "FF_"))
				flagValue := parts[1]

				var enabled bool
				var err error

				if strings.ToLower(flagValue) == "true" || flagValue == "1" {
					enabled = true
				} else if strings.ToLower(flagValue) == "false" || flagValue == "0" {
					enabled = false
				} else {
					enabled, err = strconv.ParseBool(flagValue)
					if err != nil {
						continue
					}
				}

				m.mutex.Lock()
				if flag, exists := m.flags[flagName]; exists {
					flag.Enabled = enabled
					flag.UpdatedAt = time.Now()
				} else {
					m.flags[flagName] = &Flag{
						Name:        flagName,
						Enabled:     enabled,
						Description: "Environment flag",
						UpdatedAt:   time.Now(),
					}
				}
				m.mutex.Unlock()
			}
		}
	}
}

func (m *Manager) IsEnabled(flagName string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if flag, exists := m.flags[flagName]; exists {
		return flag.Enabled
	}

	return false
}

func (m *Manager) IsEnabledWithDefault(flagName string, defaultValue bool) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if flag, exists := m.flags[flagName]; exists {
		return flag.Enabled
	}

	return defaultValue
}

func (m *Manager) GetFlag(flagName string) (*Flag, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	flag, exists := m.flags[flagName]
	return flag, exists
}

func (m *Manager) SetFlag(flagName string, enabled bool, description string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if flag, exists := m.flags[flagName]; exists {
		flag.Enabled = enabled
		flag.UpdatedAt = time.Now()
		if description != "" {
			flag.Description = description
		}
	} else {
		m.flags[flagName] = &Flag{
			Name:        flagName,
			Enabled:     enabled,
			Description: description,
			UpdatedAt:   time.Now(),
		}
	}
}

func (m *Manager) GetAllFlags() map[string]*Flag {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	result := make(map[string]*Flag)
	for name, flag := range m.flags {
		flagCopy := *flag
		result[name] = &flagCopy
	}
	return result
}

func (m *Manager) ReloadFromEnvironment() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.loadFromEnvironment()
}

// LoadDefaultFlags loads the default feature flags (for testing)
func (m *Manager) LoadDefaultFlags() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.loadDefaultFlags()
}

// LoadFromEnvironment loads flags from environment variables (for testing)
func (m *Manager) LoadFromEnvironment() {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.loadFromEnvironment()
}

func IsEnabled(flagName string) bool {
	return GetInstance().IsEnabled(flagName)
}

func IsEnabledWithDefault(flagName string, defaultValue bool) bool {
	return GetInstance().IsEnabledWithDefault(flagName, defaultValue)
}
