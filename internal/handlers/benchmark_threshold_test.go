package handlers

import (
	"testing"
)

func BenchmarkThresholdCheck_Plans_Small(b *testing.B) {
	plans := generatePlans(10)

	handler := func(c *gin.Context) {
		c.JSON(200, gin.H{"plans": plans})
	}

	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	var lastNs int64
	var lastAllocs int64

	for i := 0; i < b.N; i++ {
		handler(c)
		lastNs = b.Elapsed().Nanoseconds() / int64(b.N)
		lastAllocs = b.Elapsed().Nanoseconds() / int64(b.N)
	}

	if c, w, ok := c.(*gin.Context); ok {
		_ = w
	}
}

func TestBenchmarkThresholds_Plans(t *testing.T) {
	tests := []struct {
		name       string
		dataSize  int
		threshold BenchmarkThresholds
	}{
		{"Small", 10, ThresholdPlansSmall},
		{"Medium", 100, ThresholdPlansMedium},
		{"Large", 1000, ThresholdPlansLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plans := generatePlans(tt.dataSize)

			handler := func(c *gin.Context) {
				c.JSON(200, gin.H{"plans": plans})
			}

			c, w := setupBenchmarkContext()
			handler(c)

			if w.Code != 200 {
				t.Errorf("expected status 200, got %d", w.Code)
			}
		})
	}
}

func TestBenchmarkThresholds_Subscriptions(t *testing.T) {
	tests := []struct {
		name       string
		dataSize  int
		threshold BenchmarkThresholds
	}{
		{"Small", 10, ThresholdSubscriptionsSmall},
		{"Medium", 100, ThresholdSubscriptionsMedium},
		{"Large", 1000, ThresholdSubscriptionsLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subscriptions := generateSubscriptions(tt.dataSize)

			handler := func(c *gin.Context) {
				c.JSON(200, gin.H{"subscriptions": subscriptions})
			}

			c, w := setupBenchmarkContext()
			handler(c)

			if w.Code != 200 {
				t.Errorf("expected status 200, got %d", w.Code)
			}
		})
	}
}

func BenchmarkEnforceThreshold_Plans_Small(b *testing.B) {
	plans := generatePlans(10)

	handler := func(c *gin.Context) {
		c.JSON(200, gin.H{"plans": plans})
	}
	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}

	ns := b.Elapsed().Nanoseconds()
	if ns > int64(b.N)*ThresholdPlansSmall.MaxLatencyNs {
		b.Fatalf("latency %dns exceeds threshold %dns", ns/b.N, ThresholdPlansSmall.MaxLatencyNs)
	}
}

func BenchmarkEnforceThreshold_Plans_Medium(b *testing.B) {
	plans := generatePlans(100)

	handler := func(c *gin.Context) {
		c.JSON(200, gin.H{"plans": plans})
	}
	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}

	ns := b.Elapsed().Nanoseconds()
	if ns > int64(b.N)*ThresholdPlansMedium.MaxLatencyNs {
		b.Fatalf("latency %dns exceeds threshold %dns", ns/b.N, ThresholdPlansMedium.MaxLatencyNs)
	}
}

func BenchmarkEnforceThreshold_Subscriptions_Small(b *testing.B) {
	subscriptions := generateSubscriptions(10)

	handler := func(c *gin.Context) {
		c.JSON(200, gin.H{"subscriptions": subscriptions})
	}
	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}

	ns := b.Elapsed().Nanoseconds()
	if ns > int64(b.N)*ThresholdSubscriptionsSmall.MaxLatencyNs {
		b.Fatalf("latency %dns exceeds threshold %dns", ns/b.N, ThresholdSubscriptionsSmall.MaxLatencyNs)
	}
}

func BenchmarkEnforceThreshold_Subscriptions_Medium(b *testing.B) {
	subscriptions := generateSubscriptions(100)

	handler := func(c *gin.Context) {
		c.JSON(200, gin.H{"subscriptions": subscriptions})
	}
	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}

	ns := b.Elapsed().Nanoseconds()
	if ns > int64(b.N)*ThresholdSubscriptionsMedium.MaxLatencyNs {
		b.Fatalf("latency %dns exceeds threshold %dns", ns/b.N, ThresholdSubscriptionsMedium.MaxLatencyNs)
	}
}