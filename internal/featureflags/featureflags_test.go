package featureflags

import (
	"fmt"
	"os"
	"sync"
	"testing"
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
		t.Error("Should return default true")
	}

	if enabled := manager.IsEnabledWithDefault("nonexistent_flag", false); enabled {
		t.Error("Should return default false")
	}
}

func TestSetFlag(t *testing.T) {
	manager := GetInstance()

	manager.SetFlag("test_flag", true, "Test flag")

	flag, exists := manager.GetFlag("test_flag")
	if !exists || !flag.Enabled {
		t.Error("Flag should be enabled")
	}

	manager.SetFlag("test_flag", false, "")
	flag, _ = manager.GetFlag("test_flag")
	if flag.Enabled {
		t.Error("Flag should be disabled")
	}
}

func TestGetAllFlags(t *testing.T) {
	manager := GetInstance()

	// Create the flag FIRST
	manager.SetFlag("copy_test", true, "")

	flags := manager.GetAllFlags()

	// Now safe to modify
	flags["copy_test"].Enabled = false

	original := manager.GetAllFlags()

	if !original["copy_test"].Enabled {
		t.Error("Returned map should not affect original")
	}
	flag, ok := flags["copy_test"]
	if !ok || flag == nil {
		t.Fatal("copy_test flag missing")
	}
	flag.Enabled = false
}

func TestLoadFromEnvironment_JSON(t *testing.T) {
	os.Setenv("FEATURE_FLAGS", `{"json_flag": true}`)
	defer os.Unsetenv("FEATURE_FLAGS")

	manager := &Manager{
		flags: make(map[string]*Flag),
		db:    make(map[string]bool),
	}
	manager.LoadFromEnvironment()

	if !manager.IsEnabled("json_flag") {
		t.Error("JSON flag should be true")
	}
}

func TestLoadFromEnvironment_FF_Prefix(t *testing.T) {
	os.Setenv("FF_TEST_TRUE", "true")
	os.Setenv("FF_TEST_FALSE", "false")
	defer func() {
		os.Unsetenv("FF_TEST_TRUE")
		os.Unsetenv("FF_TEST_FALSE")
	}()

	manager := &Manager{
		flags: make(map[string]*Flag),
		db:    make(map[string]bool),
	}
	manager.LoadFromEnvironment()

	if !manager.IsEnabled("test_true") {
		t.Error("Expected true")
	}
	if manager.IsEnabled("test_false") {
		t.Error("Expected false")
	}
}

func TestConcurrentAccess(t *testing.T) {
	manager := GetInstance()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)

		go func(i int) {
			defer wg.Done()
			manager.SetFlag(fmt.Sprintf("flag_%d", i), true, "")
		}(i)

		go func(i int) {
			defer wg.Done()
			manager.IsEnabled(fmt.Sprintf("flag_%d", i))
		}(i)
	}

	wg.Wait()
}

func TestReloadFromEnvironment(t *testing.T) {
	manager := GetInstance()

	os.Setenv("FF_RELOAD_TEST", "true")
	defer os.Unsetenv("FF_RELOAD_TEST")

	manager.ReloadFromEnvironment()

	if !manager.IsEnabled("reload_test") {
		t.Error("Should reload env flag")
	}
}

func TestGlobalFunctions(t *testing.T) {
	manager := GetInstance()
	manager.SetFlag("global_test", true, "")

	if !IsEnabled("global_test") {
		t.Error("Global IsEnabled failed")
	}

	if !IsEnabledWithDefault("global_test", false) {
		t.Error("Global IsEnabledWithDefault failed")
	}
}

//
// 🔥 NEW TESTS (for 95% coverage)
//

func TestUnknownFlag(t *testing.T) {
	manager := GetInstance()

	if manager.IsEnabled("unknown_flag") {
		t.Error("Unknown flag should be false")
	}
}

func TestDBOverride(t *testing.T) {
	manager := GetInstance()

	manager.SetFlag("override_test", false, "")
	manager.SetDBFlag("override_test", true)

	if !manager.IsEnabled("override_test") {
		t.Error("DB should override config")
	}
}

func TestEnvOverride(t *testing.T) {
	os.Setenv("FF_ENV_OVERRIDE", "true")
	defer os.Unsetenv("FF_ENV_OVERRIDE")

	manager := GetInstance()

	if !manager.IsEnabled("env_override") {
		t.Error("ENV should override all")
	}
}

func TestInvalidEnvValue(t *testing.T) {
	os.Setenv("FF_BAD_FLAG", "invalid")
	defer os.Unsetenv("FF_BAD_FLAG")

	manager := GetInstance()

	if manager.IsEnabled("bad_flag") {
		t.Error("Invalid env should fallback to false")
	}
}

func TestSafeFlagProtection(t *testing.T) {
	manager := GetInstance()

	manager.SetFlag("subscriptions_enabled", false, "")

	if !manager.IsEnabled("subscriptions_enabled") {
		t.Error("Critical flag should not be disabled")
	}
}
