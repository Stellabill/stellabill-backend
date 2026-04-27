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
	db    map[string]bool // NEW: DB layer
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
			db:    make(map[string]bool),
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
	// JSON-based env
	if flagsJSON := os.Getenv("FEATURE_FLAGS"); flagsJSON != "" {
		var envFlags map[string]bool
		if err := json.Unmarshal([]byte(flagsJSON), &envFlags); err == nil {
			for name, enabled := range envFlags {
				m.mutex.Lock()
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
				m.mutex.Unlock()
			}
		}
	}

	// FF_ prefix env
	for _, env := range os.Environ() {
		if strings.HasPrefix(env, "FF_") {
			parts := strings.SplitN(env, "=", 2)
			if len(parts) == 2 {
				flagName := strings.ToLower(strings.TrimPrefix(parts[0], "FF_"))
				flagValue := parts[1]

				enabled, err := strconv.ParseBool(flagValue)
				if err != nil {
					continue
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

// NEW: DB setter
func (m *Manager) SetDBFlag(flagName string, enabled bool) {
	m.mutex.Lock()
	dirty := m.db[flagName] != enabled
	m.db[flagName] = enabled
	m.mutex.Unlock()
	
	if dirty {
		// Log if needed
	}
}

// CORE: evaluation with precedence
func (m *Manager) IsEnabled(flagName string) bool {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	value := false
	source := "default"

	// 1. ENV
	if val, ok := os.LookupEnv("FF_" + strings.ToUpper(flagName)); ok {
		if parsed, err := strconv.ParseBool(val); err == nil {
			value = parsed
			source = "env"
		}
	}

	// 2. DB
	if source == "default" {
		if val, ok := m.db[flagName]; ok {
			value = val
			source = "db"
		}
	}

	// 3. CONFIG
	if source == "default" {
		if flag, exists := m.flags[flagName]; exists {
			value = flag.Enabled
			source = "config"

			// 🔐 SECURITY: protect critical flags
			if strings.Contains(flag.Name, "subscriptions") && !value {
				value = true
				source = "forced-safe"
			}
		}
	}

	// 4. DEFAULT = false

	m.sampleLog(flagName, value, source)
	return value
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
		copy := *flag
		result[name] = &copy
	}
	return result
}

// FIXED: no double locking
func (m *Manager) ReloadFromEnvironment() {
	m.loadFromEnvironment()
}

// FIXED
func (m *Manager) LoadDefaultFlags() {
	m.loadDefaultFlags()
}

// FIXED
func (m *Manager) LoadFromEnvironment() {
	m.loadFromEnvironment()
}

// NEW: sampled logging
func (m *Manager) sampleLog(name string, value bool, source string) {
	if time.Now().UnixNano()%10 == 0 {
		fmt.Printf("[feature_flag] %s=%v (%s)\n", name, value, source)
	}
}

// Global helpers
func IsEnabled(flagName string) bool {
	return GetInstance().IsEnabled(flagName)
}

func IsEnabledWithDefault(flagName string, defaultValue bool) bool {
	return GetInstance().IsEnabledWithDefault(flagName, defaultValue)
}

func SetFlag(flagName string, enabled bool, description string) {
	GetInstance().SetFlag(flagName, enabled, description)
}
