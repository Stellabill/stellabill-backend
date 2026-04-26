package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// Benchmark fixtures for subscriptions
func generateSubscriptions(count int) []Subscription {
	subscriptions := make([]Subscription, count)
	for i := 0; i < count; i++ {
		subscriptions[i] = Subscription{
			ID:          generateID("sub", i),
			PlanID:      generateID("plan", i%10),
			Customer:    generateCustomerID(i),
			Status:      generateStatus(i),
			Amount:      generateAmount(i),
			Interval:    generateInterval(i),
			NextBilling: generateNextBilling(i),
		}
	}
	return subscriptions
}

func generateCustomerID(i int) string {
	return "cust-" + itoa(i)
}

func generateStatus(i int) string {
	statuses := []string{"active", "past_due", "canceled", "trialing"}
	return statuses[i%len(statuses)]
}

func generateNextBilling(i int) string {
	if i%4 == 0 {
		return "2024-04-01T00:00:00Z"
	}
	if i%4 == 1 {
		return "2024-05-01T00:00:00Z"
	}
	if i%4 == 2 {
		return "2024-06-01T00:00:00Z"
	}
	return ""
}

// BenchmarkListSubscriptions_Empty tests performance with no data
func BenchmarkListSubscriptions_Empty(b *testing.B) {
	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ListSubscriptions(c)
	}
}

// BenchmarkListSubscriptions_Small tests performance with 10 subscriptions
func BenchmarkListSubscriptions_Small(b *testing.B) {
	subscriptions := generateSubscriptions(10)

	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	}

	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkListSubscriptions_Medium tests performance with 100 subscriptions
func BenchmarkListSubscriptions_Medium(b *testing.B) {
	subscriptions := generateSubscriptions(100)

	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	}

	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkListSubscriptions_Large tests performance with 1000 subscriptions
func BenchmarkListSubscriptions_Large(b *testing.B) {
	subscriptions := generateSubscriptions(1000)

	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	}

	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkListSubscriptions_ExtraLarge tests performance with 10000 subscriptions
func BenchmarkListSubscriptions_ExtraLarge(b *testing.B) {
	subscriptions := generateSubscriptions(10000)

	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	}

	c, _ := setupBenchmarkContext()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkListSubscriptions_JSONEncoding tests JSON encoding performance
func BenchmarkListSubscriptions_JSONEncoding_Small(b *testing.B) {
	subscriptions := generateSubscriptions(10)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(gin.H{"subscriptions": subscriptions})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListSubscriptions_JSONEncoding_Medium(b *testing.B) {
	subscriptions := generateSubscriptions(100)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(gin.H{"subscriptions": subscriptions})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListSubscriptions_JSONEncoding_Large(b *testing.B) {
	subscriptions := generateSubscriptions(1000)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(gin.H{"subscriptions": subscriptions})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkListSubscriptions_FullHTTP tests full HTTP request/response cycle
func BenchmarkListSubscriptions_FullHTTP_Small(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	subscriptions := generateSubscriptions(10)
	router.GET("/api/subscriptions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/subscriptions", nil)
		router.ServeHTTP(w, req)
	}
}

func BenchmarkListSubscriptions_FullHTTP_Medium(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	subscriptions := generateSubscriptions(100)
	router.GET("/api/subscriptions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/subscriptions", nil)
		router.ServeHTTP(w, req)
	}
}

func BenchmarkListSubscriptions_FullHTTP_Large(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	subscriptions := generateSubscriptions(1000)
	router.GET("/api/subscriptions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/subscriptions", nil)
		router.ServeHTTP(w, req)
	}
}

// BenchmarkListSubscriptions_Parallel tests concurrent request handling
func BenchmarkListSubscriptions_Parallel_Small(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	subscriptions := generateSubscriptions(10)
	router.GET("/api/subscriptions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	})

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/subscriptions", nil)
			router.ServeHTTP(w, req)
		}
	})
}

func BenchmarkListSubscriptions_Parallel_Medium(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	subscriptions := generateSubscriptions(100)
	router.GET("/api/subscriptions", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"subscriptions": subscriptions})
	})

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/subscriptions", nil)
			router.ServeHTTP(w, req)
		}
	})
}

// BenchmarkGetSubscription tests single subscription retrieval
func BenchmarkGetSubscription_FullHTTP(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("callerID", "bench-caller")
		c.Next()
	})
	router.GET("/api/subscriptions/:id", NewGetSubscriptionHandler(&mockSubscriptionService{}))

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/subscriptions/sub-123", nil)
		router.ServeHTTP(w, req)
	}
}

// BenchmarkListSubscriptions_FilteredByStatus simulates filtering
func BenchmarkListSubscriptions_FilteredByStatus_Medium(b *testing.B) {
	subscriptions := generateSubscriptions(100)

	// Simulate filtering by status
	handler := func(c *gin.Context) {
		status := c.Query("status")
		filtered := subscriptions

		if status != "" {
			filtered = make([]Subscription, 0)
			for _, sub := range subscriptions {
				if sub.Status == status {
					filtered = append(filtered, sub)
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"subscriptions": filtered})
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/subscriptions", handler)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/subscriptions?status=active", nil)
		router.ServeHTTP(w, req)
	}
}

func BenchmarkListSubscriptions_FilteredByStatus_Large(b *testing.B) {
	subscriptions := generateSubscriptions(1000)

	handler := func(c *gin.Context) {
		status := c.Query("status")
		filtered := subscriptions

		if status != "" {
			filtered = make([]Subscription, 0)
			for _, sub := range subscriptions {
				if sub.Status == status {
					filtered = append(filtered, sub)
				}
			}
		}

		c.JSON(http.StatusOK, gin.H{"subscriptions": filtered})
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/subscriptions", handler)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/subscriptions?status=active", nil)
		router.ServeHTTP(w, req)
	}
}

func BenchmarkListSubscriptions_LargeDataset(b *testing.B) {
	// simulate large dataset
	size := 10000

	data := make([]Subscription, size)
	for i := 0; i < size; i++ {
		data[i] = Subscription{
			ID:       fmt.Sprintf("sub-%d", i),
			Customer: "cust-1",
			Status:   "active",
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		start := time.Now()

		_ = filterSubscriptions(data, "cust-1")

		if time.Since(start) > 50*time.Millisecond {
			b.Fatalf("query too slow: %v", time.Since(start))
		}
	}
}
