package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// BenchmarkBaseline establishes baseline performance metrics
func BenchmarkBaseline_EmptyHandler(b *testing.B) {
	c, _ := setupBenchmarkContext()

	handler := func(c *gin.Context) {
		c.JSON(200, gin.H{})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkComparison_PlansVsSubscriptions compares relative performance
func BenchmarkComparison_PlansVsSubscriptions_Medium(b *testing.B) {
	plans := generatePlans(100)
	subscriptions := generateSubscriptions(100)

	b.Run("Plans", func(b *testing.B) {
		c, _ := setupBenchmarkContext()
		handler := func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"plans": plans})
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			handler(c)
		}
	})

	b.Run("Subscriptions", func(b *testing.B) {
		c, _ := setupBenchmarkContext()
		handler := func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			handler(c)
		}
	})
}

// BenchmarkMemoryAllocation tracks allocation patterns
func BenchmarkMemoryAllocation_Plans(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000}

	for _, size := range sizes {
		b.Run(itoa(size), func(b *testing.B) {
			plans := generatePlans(size)
			c, _ := setupBenchmarkContext()

			handler := func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"plans": plans})
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				handler(c)
			}
		})
	}
}

func BenchmarkMemoryAllocation_Subscriptions(b *testing.B) {
	sizes := []int{10, 100, 1000, 10000}

	for _, size := range sizes {
		b.Run(itoa(size), func(b *testing.B) {
			subscriptions := generateSubscriptions(size)
			c, _ := setupBenchmarkContext()

			handler := func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				handler(c)
			}
		})
	}
}

// BenchmarkConcurrency tests performance under concurrent load
func BenchmarkConcurrency_Plans_Medium(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	plans := generatePlans(100)
	router.GET("/api/plans", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	})

	concurrencyLevels := []int{1, 10, 100}

	for _, level := range concurrencyLevels {
		b.Run("Concurrency"+itoa(level), func(b *testing.B) {
			b.SetParallelism(level)
			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					w := httptest.NewRecorder()
					req := httptest.NewRequest("GET", "/api/plans", nil)
					router.ServeHTTP(w, req)
				}
			})
		})
	}
}

func BenchmarkConcurrency_Subscriptions_Medium(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	subscriptions := generateSubscriptions(100)
	router.GET("/api/subscriptions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	})

	concurrencyLevels := []int{1, 10, 100}

	for _, level := range concurrencyLevels {
		b.Run("Concurrency"+itoa(level), func(b *testing.B) {
			b.SetParallelism(level)
			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					w := httptest.NewRecorder()
					req := httptest.NewRequest("GET", "/api/subscriptions", nil)
					router.ServeHTTP(w, req)
				}
			})
		})
	}
}
