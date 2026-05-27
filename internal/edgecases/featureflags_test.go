package edgecases

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"stellarbill-backend/internal/featureflags"
)

func TestUnknownFlagBehavior(t *testing.T) {
	manager := featureflags.GetInstance()
	
	if enabled := manager.IsEnabled("completely_unknown_flag"); enabled {
		t.Error("Unknown flag should return false by default")
	}
	
	if enabled := manager.IsEnabledWithDefault("completely_unknown_flag", true); !enabled {
		t.Error("Unknown flag should return provided default")
	}
	
	if enabled := manager.IsEnabledWithDefault("completely_unknown_flag", false); enabled {
		t.Error("Unknown flag should return provided default")
	}
}

func TestInvalidJSONFeatureFlags(t *testing.T) {
	invalidJSON := `{"invalid": json}`
	os.Setenv("FEATURE_FLAGS", invalidJSON)
	defer os.Unsetenv("FEATURE_FLAGS")
	
	manager := featureflags.NewManager()
	
	manager.LoadFromEnvironment()
	
	if len(manager.GetAllFlags()) != 0 {
		t.Error("Invalid JSON should not create any flags")
	}
}

func TestMalformedEnvironmentVariables(t *testing.T) {
	testCases := []struct {
		name   string
		envVar string
		value  string
	}{
		{"empty_value", "FF_EMPTY_FLAG", ""},
		{"invalid_bool", "FF_INVALID_BOOL", "maybe"},
		{"numeric_invalid", "FF_NUMERIC_INVALID", "42"},
		{"special_chars", "FF_SPECIAL", "!@#$%^&*()"},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv(tc.envVar, tc.value)
			defer os.Unsetenv(tc.envVar)
			
			manager := featureflags.NewManager()
			
			manager.LoadFromEnvironment()
			
			flagName := tc.envVar[3:] // Remove "FF_" prefix
			flagName = strings.ToLower(flagName)
			
			if _, exists := manager.GetFlag(flagName); exists {
				t.Errorf("Malformed flag %s should not be created", tc.name)
			}
		})
	}
}

func TestConcurrentFlagModifications(t *testing.T) {
	manager := featureflags.GetInstance()
	
	const numGoroutines = 50
	const numOperations = 100
	
	done := make(chan bool, numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			for j := 0; j < numOperations; j++ {
				flagName := fmt.Sprintf("concurrent_edge_%d", id)
				manager.SetFlag(flagName, j%2 == 0, "")
				manager.IsEnabled(flagName)
				manager.GetFlag(flagName)
			}
			done <- true
		}(i)
	}
	
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
	
	flags := manager.GetAllFlags()
	for i := 0; i < numGoroutines; i++ {
		flagName := fmt.Sprintf("concurrent_edge_%d", i)
		if _, exists := flags[flagName]; !exists {
			t.Errorf("Flag %s should exist after concurrent operations", flagName)
		}
	}
}

func TestStaleConfigHandling(t *testing.T) {
	manager := featureflags.GetInstance()
	
	// Set initial state
	manager.SetFlag("stale_test", true, "Initial description")
	
	// Simulate time passing
	time.Sleep(10 * time.Millisecond)
	
	// Create a new manager instance (simulating app restart)
	manager2 := featureflags.NewManager()
	manager2.LoadDefaultFlags()
	
	// The new manager should have default state, not the stale state
	if enabled := manager2.IsEnabled("stale_test"); enabled {
		t.Error("New manager instance should not have stale flag state")
	}
}

func TestDynamicToggles(t *testing.T) {
	manager := featureflags.GetInstance()
	
	// Test rapid toggling
	for i := 0; i < 100; i++ {
		expected := i%2 == 0
		manager.SetFlag("dynamic_toggle", expected, "")
		
		if enabled := manager.IsEnabled("dynamic_toggle"); enabled != expected {
			t.Errorf("Expected dynamic_toggle to be %v at iteration %d, got %v", expected, i, enabled)
		}
	}
}

func TestMemoryLeakPrevention(t *testing.T) {
	manager := featureflags.GetInstance()
	
	initialFlagCount := len(manager.GetAllFlags())
	
	// Create and delete many flags
	for i := 0; i < 1000; i++ {
		flagName := fmt.Sprintf("temp_flag_%d", i)
		manager.SetFlag(flagName, true, "Temporary flag")
	}
	
	// Check that flags were created
	afterCreationCount := len(manager.GetAllFlags())
	if afterCreationCount <= initialFlagCount {
		t.Error("Flags should have been created")
	}
	
	// Simulate cleanup by setting flags to nil (in real implementation, you'd have a delete method)
	// For now, we just verify that the map doesn't grow unboundedly with repeated operations
	
	// Repeat operations on same flags
	for i := 0; i < 1000; i++ {
		flagName := fmt.Sprintf("temp_flag_%d", i%100) // Reuse first 100 flags
		manager.SetFlag(flagName, i%2 == 0, "Updated description")
	}
	
	finalCount := len(manager.GetAllFlags())
	if finalCount != afterCreationCount {
		t.Error("Reusing existing flags should not increase count")
	}
}

func TestSpecialCharactersInFlagNames(t *testing.T) {
	manager := featureflags.GetInstance()
	
	specialNames := []string{
		"flag-with-dashes",
		"flag_with_underscores",
		"flag.with.dots",
		"flag with spaces",
		"flag-with-numbers-123",
		"FLAG_IN_UPPERCASE",
		"mixed-CASE_Flag",
	}
	
	for _, name := range specialNames {
		manager.SetFlag(name, true, "Test special characters")
		
		if !manager.IsEnabled(name) {
			t.Errorf("Flag with special characters '%s' should be enabled", name)
		}
		
		flag, exists := manager.GetFlag(name)
		if !exists {
			t.Errorf("Flag with special characters '%s' should exist", name)
		}
		
		if flag.Name != name {
			t.Errorf("Flag name should preserve special characters: expected '%s', got '%s'", name, flag.Name)
		}
	}
}

func TestEnvironmentVariablePrecedence(t *testing.T) {
	// Test that FF_ prefix takes precedence over FEATURE_FLAGS JSON
	os.Setenv("FEATURE_FLAGS", `{"precedence_test": false}`)
	os.Setenv("FF_PRECEDENCE_TEST", "true")
	defer func() {
		os.Unsetenv("FEATURE_FLAGS")
		os.Unsetenv("FF_PRECEDENCE_TEST")
	}()
	
	manager := featureflags.NewManager()
	manager.LoadDefaultFlags()
	manager.LoadFromEnvironment()
	
	if !manager.IsEnabled("precedence_test") {
		t.Error("FF_ prefix should take precedence over FEATURE_FLAGS JSON")
	}
}

func TestLargeFlagConfiguration(t *testing.T) {
	// Test with a large number of flags
	var largeFlags map[string]bool
	largeFlags = make(map[string]bool)
	
	for i := 0; i < 1000; i++ {
		flagName := fmt.Sprintf("large_flag_%d", i)
		largeFlags[flagName] = i%2 == 0
	}
	
	flagsJSON, _ := json.Marshal(largeFlags)
	os.Setenv("FEATURE_FLAGS", string(flagsJSON))
	defer os.Unsetenv("FEATURE_FLAGS")
	
	manager := featureflags.NewManager()
	manager.LoadDefaultFlags()
	manager.LoadFromEnvironment()
	
	allFlags := manager.GetAllFlags()
	
	// Check that all large flags were loaded
	for i := 0; i < 1000; i++ {
		flagName := fmt.Sprintf("large_flag_%d", i)
		flag, exists := allFlags[flagName]
		if !exists {
			t.Errorf("Large flag %s should exist", flagName)
		} else if flag.Enabled != (i%2 == 0) {
			t.Errorf("Large flag %s should have enabled=%v", flagName, i%2 == 0)
		}
	}
}

func TestRaceConditionInReload(t *testing.T) {
	manager := featureflags.GetInstance()
	
	manager.SetFlag("race_test", false, "")
	
	done := make(chan bool, 2)
	
	// Goroutine 1: Reload from environment
	go func() {
		for i := 0; i < 100; i++ {
			os.Setenv("FF_RACE_TEST", "true")
			manager.ReloadFromEnvironment()
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()
	
	// Goroutine 2: Check flag status
	go func() {
		for i := 0; i < 100; i++ {
			manager.IsEnabled("race_test")
			time.Sleep(time.Microsecond)
		}
		done <- true
	}()
	
	<-done
	<-done
	
	os.Unsetenv("FF_RACE_TEST")
}
