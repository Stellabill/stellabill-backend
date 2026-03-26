package main

import (
	"fmt"
	"os"

	"stellarbill-backend/internal/featureflags"
)

func main() {
	// Test basic functionality
	fmt.Println("=== Feature Flags Demo ===")
	
	// Test default flags
	fmt.Printf("subscriptions_enabled (default): %v\n", featureflags.IsEnabled("subscriptions_enabled"))
	fmt.Printf("new_billing_flow (default): %v\n", featureflags.IsEnabled("new_billing_flow"))
	
	// Test environment override
	os.Setenv("FF_TEST_DEMO", "true")
	manager := featureflags.GetInstance()
	manager.ReloadFromEnvironment()
	fmt.Printf("FF_TEST_DEMO (from env): %v\n", featureflags.IsEnabled("test_demo"))
	
	// Test unknown flag with default
	fmt.Printf("unknown_flag (default false): %v\n", featureflags.IsEnabledWithDefault("unknown_flag", false))
	fmt.Printf("unknown_flag (default true): %v\n", featureflags.IsEnabledWithDefault("unknown_flag", true))
	
	// Show all flags
	fmt.Println("\n=== All Flags ===")
	allFlags := manager.GetAllFlags()
	for name, flag := range allFlags {
		fmt.Printf("%s: %v (%s)\n", name, flag.Enabled, flag.Description)
	}
	
	fmt.Println("\n=== Demo Complete ===")
}
