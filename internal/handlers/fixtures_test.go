package handlers

import (
	"testing"
)

// TestFixtureGeneration verifies fixture generators produce valid data
func TestGeneratePlans(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{"Empty", 0},
		{"Small", 10},
		{"Medium", 100},
		{"Large", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plans := generatePlans(tt.count)

			if len(plans) != tt.count {
				t.Errorf("Expected %d plans, got %d", tt.count, len(plans))
			}

			// Verify all plans have required fields
			for i, plan := range plans {
				if plan.ID == "" {
					t.Errorf("Plan %d missing ID", i)
				}
				if plan.Name == "" {
					t.Errorf("Plan %d missing Name", i)
				}
				if plan.Amount == "" {
					t.Errorf("Plan %d missing Amount", i)
				}
				if plan.Currency == "" {
					t.Errorf("Plan %d missing Currency", i)
				}
				if plan.Interval == "" {
					t.Errorf("Plan %d missing Interval", i)
				}
			}
		})
	}
}

func TestGenerateSubscriptions(t *testing.T) {
	tests := []struct {
		name  string
		count int
	}{
		{"Empty", 0},
		{"Small", 10},
		{"Medium", 100},
		{"Large", 1000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subscriptions := generateSubscriptions(tt.count)

			if len(subscriptions) != tt.count {
				t.Errorf("Expected %d subscriptions, got %d", tt.count, len(subscriptions))
			}

			// Verify all subscriptions have required fields
			for i, sub := range subscriptions {
				if sub.ID == "" {
					t.Errorf("Subscription %d missing ID", i)
				}
				if sub.PlanID == "" {
					t.Errorf("Subscription %d missing PlanID", i)
				}
				if sub.Customer == "" {
					t.Errorf("Subscription %d missing Customer", i)
				}
				if sub.Status == "" {
					t.Errorf("Subscription %d missing Status", i)
				}
				if sub.Amount == "" {
					t.Errorf("Subscription %d missing Amount", i)
				}
				if sub.Interval == "" {
					t.Errorf("Subscription %d missing Interval", i)
				}
			}
		})
	}
}

func TestFixtureHelpers(t *testing.T) {
	t.Run("generateID", func(t *testing.T) {
		id := generateID("test", 123)
		if id != "test-123" {
			t.Errorf("Expected 'test-123', got '%s'", id)
		}
	})

	t.Run("generateName", func(t *testing.T) {
		name := generateName("Plan", 5)
		if name != "Plan 5" {
			t.Errorf("Expected 'Plan 5', got '%s'", name)
		}
	})

	t.Run("generateAmount", func(t *testing.T) {
		amount := generateAmount(0)
		if amount == "" {
			t.Error("Expected non-empty amount")
		}
	})

	t.Run("generateInterval", func(t *testing.T) {
		interval := generateInterval(0)
		validIntervals := map[string]bool{
			"month": true, "year": true, "week": true, "quarter": true,
		}
		if !validIntervals[interval] {
			t.Errorf("Invalid interval: %s", interval)
		}
	})

	t.Run("generateStatus", func(t *testing.T) {
		status := generateStatus(0)
		validStatuses := map[string]bool{
			"active": true, "past_due": true, "canceled": true, "trialing": true,
		}
		if !validStatuses[status] {
			t.Errorf("Invalid status: %s", status)
		}
	})

	t.Run("itoa", func(t *testing.T) {
		tests := []struct {
			input    int
			expected string
		}{
			{0, "0"},
			{1, "1"},
			{10, "10"},
			{123, "123"},
			{9999, "9999"},
		}

		for _, tt := range tests {
			result := itoa(tt.input)
			if result != tt.expected {
				t.Errorf("itoa(%d) = %s, expected %s", tt.input, result, tt.expected)
			}
		}
	})
}

func TestFixtureDataDistribution(t *testing.T) {
	t.Run("Plans have varied intervals", func(t *testing.T) {
		plans := generatePlans(100)
		intervals := make(map[string]int)

		for _, plan := range plans {
			intervals[plan.Interval]++
		}

		if len(intervals) < 2 {
			t.Error("Expected multiple interval types")
		}
	})

	t.Run("Subscriptions have varied statuses", func(t *testing.T) {
		subscriptions := generateSubscriptions(100)
		statuses := make(map[string]int)

		for _, sub := range subscriptions {
			statuses[sub.Status]++
		}

		if len(statuses) < 2 {
			t.Error("Expected multiple status types")
		}
	})
}

