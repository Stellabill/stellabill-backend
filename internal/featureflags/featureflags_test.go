package featureflags

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestGetInstance(t *testing.T) {
	manager1 := GetInstance()
	manager2 := GetInstance()

	if manager1 != manager2 {
		t.Error("GetInstance should return the same singleton instance")
	}
}

func TestDefaultFlags(t *testing.T) {
	manager := GetInstance()

	tests := []struct {
		flagName string
		expected bool
	}{
		{"subscriptions_enabled", true},
		{"plans_enabled", true},
		{"new_billing_flow", false},
		{"advanced_analytics", false},
	}

	for _, test := range tests {
		t.Run(test.flagName, func(t *testing.T) {
			if enabled := manager.IsEnabled(test.flagName); enabled != test.expected {
				t.Errorf("Expected flag %s to be %v, got %v", test.flagName, test.expected, enabled)
			}
		})
	}
}

func TestIsEnabledWithDefault(t *testing.T) {
	manager := GetInstance()

	if enabled := manager.IsEnabledWithDefault("nonexistent_flag", true); !enabled {
		t.Error("IsEnabledWithDefault should return default value for nonexistent flag")
	}

	if enabled := manager.IsEnabledWithDefault("nonexistent_flag", false); enabled {
		t.Error("IsEnabledWithDefault should return default value for nonexistent flag")
	}
}

func TestSetFlag(t *testing.T) {
	manager := GetInstance()

	manager.SetFlag("test_flag", true, "Test flag for unit testing")

	if flag, exists := manager.GetFlag("test_flag"); !exists {
		t.Error("Flag should exist after setting")
	} else {
		if !flag.Enabled {
			t.Error("Flag should be enabled")
		}
		if flag.Description != "Test flag for unit testing" {
			t.Error("Flag description should match")
		}
	}

	manager.SetFlag("test_flag", false, "")
	if flag, exists := manager.GetFlag("test_flag"); !exists {
		t.Error("Flag should still exist")
	} else if flag.Enabled {
		t.Error("Flag should be disabled")
	}
}

func TestGetAllFlags(t *testing.T) {
	manager := GetInstance()

	flags := manager.GetAllFlags()
	if len(flags) == 0 {
		t.Error("Should have default flags")
	}

	originalCount := len(flags)
	manager.SetFlag("another_test_flag", true, "Another test")

	flags = manager.GetAllFlags()
	if len(flags) != originalCount+1 {
		t.Error("Should have one more flag")
	}

	flags["another_test_flag"].Enabled = false
	originalFlags := manager.GetAllFlags()
	if originalFlags["another_test_flag"].Enabled {
		t.Error("Modifying returned flags should not affect original")
	}
}

func TestLoadFromEnvironment_JSON(t *testing.T) {
	jsonData := `{"test_env_flag": true, "another_env_flag": false}`
	os.Setenv("FEATURE_FLAGS", jsonData)
	defer os.Unsetenv("FEATURE_FLAGS")

	manager := &Manager{
		flags: make(map[string]*Flag),
	}
	manager.loadFromEnvironment()

	if !manager.IsEnabled("test_env_flag") {
		t.Error("JSON flag should be enabled")
	}

	if manager.IsEnabled("another_env_flag") {
		t.Error("JSON flag should be disabled")
	}
}

func TestLoadFromEnvironment_FF_Prefix(t *testing.T) {
	os.Setenv("FF_TEST_BOOL_TRUE", "true")
	os.Setenv("FF_TEST_BOOL_FALSE", "false")
	os.Setenv("FF_TEST_INT_1", "1")
	os.Setenv("FF_TEST_INT_0", "0")
	os.Setenv("FF_TEST_INVALID", "invalid")
	defer func() {
		os.Unsetenv("FF_TEST_BOOL_TRUE")
		os.Unsetenv("FF_TEST_BOOL_FALSE")
		os.Unsetenv("FF_TEST_INT_1")
		os.Unsetenv("FF_TEST_INT_0")
		os.Unsetenv("FF_TEST_INVALID")
	}()

	manager := &Manager{
		flags: make(map[string]*Flag),
	}
	manager.loadFromEnvironment()

	tests := []struct {
		flagName string
		expected bool
	}{
		{"test_bool_true", true},
		{"test_bool_false", false},
		{"test_int_1", true},
		{"test_int_0", false},
	}

	for _, test := range tests {
		if enabled := manager.IsEnabled(test.flagName); enabled != test.expected {
			t.Errorf("Expected flag %s to be %v, got %v", test.flagName, test.expected, enabled)
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	manager := GetInstance()

	var wg sync.WaitGroup
	numGoroutines := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(2)

		go func(id int) {
			defer wg.Done()
			flagName := fmt.Sprintf("concurrent_flag_%d", id)
			manager.SetFlag(flagName, true, "")
		}(i)

		go func(id int) {
			defer wg.Done()
			flagName := fmt.Sprintf("concurrent_flag_%d", id)
			manager.IsEnabled(flagName)
		}(i)
	}

	wg.Wait()

	flags := manager.GetAllFlags()
	for i := 0; i < numGoroutines; i++ {
		flagName := fmt.Sprintf("concurrent_flag_%d", i)
		if flag, exists := flags[flagName]; !exists {
			t.Errorf("Flag %s should exist", flagName)
		} else if !flag.Enabled {
			t.Errorf("Flag %s should be enabled", flagName)
		}
	}
}

func TestReloadFromEnvironment(t *testing.T) {
	manager := GetInstance()

	manager.SetFlag("reload_test", false, "")

	os.Setenv("FF_RELOAD_TEST", "true")
	defer os.Unsetenv("FF_RELOAD_TEST")

	manager.ReloadFromEnvironment()

	if !manager.IsEnabled("reload_test") {
		t.Error("Flag should be reloaded from environment")
	}
}

func TestGlobalFunctions(t *testing.T) {
	SetFlag("global_test", true, "")

	if !IsEnabled("global_test") {
		t.Error("Global IsEnabled should work")
	}

	if !IsEnabledWithDefault("global_test", false) {
		t.Error("Global IsEnabledWithDefault should work")
	}

	if IsEnabledWithDefault("nonexistent_global", false) {
		t.Error("Global IsEnabledWithDefault should return default for nonexistent flag")
	}

	if !IsEnabledWithDefault("nonexistent_global", true) {
		t.Error("Global IsEnabledWithDefault should return default for nonexistent flag")
	}
}
