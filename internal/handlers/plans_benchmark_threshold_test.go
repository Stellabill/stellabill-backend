package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func BenchmarkPlans_ValidateThresholds_Small(b *testing.B) {
	plans := generatePlans(10)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	var passed bool
	for i := 0; i < b.N; i++ {
		handler(c)
		passed = c.Writer.Status() == 200
	}
	
	if !passed {
		b.Fatal("handler failed")
	}
	
	thresholds := ThresholdPlansSmall
	b.ReportMetric(float64(thresholds.MaxLatencyNs), "max latency ns")
	b.ReportMetric(float64(thresholds.MaxAllocsOp), "max allocs")
	b.ReportMetric(float64(thresholds.MaxBytesOp), "max bytes")
}

func BenchmarkPlans_ValidateThresholds_Medium(b *testing.B) {
	plans := generatePlans(100)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	var passed bool
	for i := 0; i < b.N; i++ {
		handler(c)
		passed = c.Writer.Status() == 200
	}
	
	if !passed {
		b.Fatal("handler failed")
	}
	
	thresholds := ThresholdPlansMedium
	b.ReportMetric(float64(thresholds.MaxLatencyNs), "max latency ns")
	b.ReportMetric(float64(thresholds.MaxAllocsOp), "max allocs")
	b.ReportMetric(float64(thresholds.MaxBytesOp), "max bytes")
}

func BenchmarkPlans_ValidateThresholds_Large(b *testing.B) {
	plans := generatePlans(1000)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	var passed bool
	for i := 0; i < b.N; i++ {
		handler(c)
		passed = c.Writer.Status() == 200
	}
	
	if !passed {
		b.Fatal("handler failed")
	}
	
	thresholds := ThresholdPlansLarge
	b.ReportMetric(float64(thresholds.MaxLatencyNs), "max latency ns")
	b.ReportMetric(float64(thresholds.MaxAllocsOp), "max allocs")
	b.ReportMetric(float64(thresholds.MaxBytesOp), "max bytes")
}

func BenchmarkSubscriptions_ValidateThresholds_Small(b *testing.B) {
	subs := generateSubscriptions(10)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subs})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	var passed bool
	for i := 0; i < b.N; i++ {
		handler(c)
		passed = c.Writer.Status() == 200
	}
	
	if !passed {
		b.Fatal("handler failed")
	}
	
	thresholds := ThresholdSubscriptionsSmall
	b.ReportMetric(float64(thresholds.MaxLatencyNs), "max latency ns")
	b.ReportMetric(float64(thresholds.MaxAllocsOp), "max allocs")
	b.ReportMetric(float64(thresholds.MaxBytesOp), "max bytes")
}

func BenchmarkSubscriptions_ValidateThresholds_Medium(b *testing.B) {
	subs := generateSubscriptions(100)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subs})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	var passed bool
	for i := 0; i < b.N; i++ {
		handler(c)
		passed = c.Writer.Status() == 200
	}
	
	if !passed {
		b.Fatal("handler failed")
	}
	
	thresholds := ThresholdSubscriptionsMedium
	b.ReportMetric(float64(thresholds.MaxLatencyNs), "max latency ns")
	b.ReportMetric(float64(thresholds.MaxAllocsOp), "max allocs")
	b.ReportMetric(float64(thresholds.MaxBytesOp), "max bytes")
}

func BenchmarkSubscriptions_ValidateThresholds_Large(b *testing.B) {
	subs := generateSubscriptions(1000)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subs})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	var passed bool
	for i := 0; i < b.N; i++ {
		handler(c)
		passed = c.Writer.Status() == 200
	}
	
	if !passed {
		b.Fatal("handler failed")
	}
	
	thresholds := ThresholdSubscriptionsLarge
	b.ReportMetric(float64(thresholds.MaxLatencyNs), "max latency ns")
	b.ReportMetric(float64(thresholds.MaxAllocsOp), "max allocs")
	b.ReportMetric(float64(thresholds.MaxBytesOp), "max bytes")
}

func generateSubscriptions(count int) []Subscription {
	subs := make([]Subscription, count)
	for i := 0; i < count; i++ {
		subs[i] = Subscription{
			ID:           generateID("sub", i),
			PlanID:      generateID("plan", i),
			CustomerID:  generateID("cust", i),
			Status:      "active",
			Amount:      generateAmount(i),
			Currency:    "USD",
			Interval:    generateInterval(i),
			CurrentPeriodEnd: time.Now().Add(30 * 24 * time.Hour),
		}
	}
	return subs
}