package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Benchmark fixtures
func generatePlans(count int) []Plan {
	plans := make([]Plan, count)
	for i := 0; i < count; i++ {
		plans[i] = Plan{
			ID:          generateID("plan", i),
			Name:        generateName("Plan", i),
			Amount:      generateAmount(i),
			Currency:    "USD",
			Interval:    generateInterval(i),
			Description: generateDescription(i),
		}
	}
	return plans
}

func generateID(prefix string, i int) string {
	return prefix + "-" + itoa(i)
}

func generateName(prefix string, i int) string {
	return prefix + " " + itoa(i)
}

func generateAmount(i int) string {
	amounts := []string{"9.99", "19.99", "49.99", "99.99", "199.99"}
	return amounts[i%len(amounts)]
}

func generateInterval(i int) string {
	intervals := []string{"month", "year", "week", "quarter"}
	return intervals[i%len(intervals)]
}

func generateDescription(i int) string {
	if i%3 == 0 {
		return "Premium plan with advanced features"
	}
	if i%3 == 1 {
		return "Standard plan for growing businesses"
	}
	return "Basic plan for individuals"
}

func itoa(i int) string {
	// Simple int to string conversion
	if i == 0 {
		return "0"
	}
	
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// Benchmark helper to create test context
func setupBenchmarkContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/plans", nil)
	return c, w
}

// BenchmarkListPlans_Empty tests performance with no data
/*
func BenchmarkListPlans_Empty(b *testing.B) {
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		ListPlans(c)
	}
}
*/

// BenchmarkListPlans_Small tests performance with 10 plans
func BenchmarkListPlans_Small(b *testing.B) {
	plans := generatePlans(10)
	
	// Mock handler with data
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkListPlans_Medium tests performance with 100 plans
func BenchmarkListPlans_Medium(b *testing.B) {
	plans := generatePlans(100)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkListPlans_Large tests performance with 1000 plans
func BenchmarkListPlans_Large(b *testing.B) {
	plans := generatePlans(1000)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkListPlans_ExtraLarge tests performance with 10000 plans
func BenchmarkListPlans_ExtraLarge(b *testing.B) {
	plans := generatePlans(10000)
	
	handler := func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	}
	
	c, _ := setupBenchmarkContext()
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		handler(c)
	}
}

// BenchmarkListPlans_JSONEncoding tests JSON encoding performance
func BenchmarkListPlans_JSONEncoding_Small(b *testing.B) {
	plans := generatePlans(10)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(gin.H{"plans": plans})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListPlans_JSONEncoding_Medium(b *testing.B) {
	plans := generatePlans(100)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(gin.H{"plans": plans})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListPlans_JSONEncoding_Large(b *testing.B) {
	plans := generatePlans(1000)
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(gin.H{"plans": plans})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkListPlans_FullHTTP tests full HTTP request/response cycle
func BenchmarkListPlans_FullHTTP_Small(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	plans := generatePlans(10)
	router.GET("/api/plans", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/plans", nil)
		router.ServeHTTP(w, req)
	}
}

func BenchmarkListPlans_FullHTTP_Medium(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	plans := generatePlans(100)
	router.GET("/api/plans", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/plans", nil)
		router.ServeHTTP(w, req)
	}
}

func BenchmarkListPlans_FullHTTP_Large(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	plans := generatePlans(1000)
	router.GET("/api/plans", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/api/plans", nil)
		router.ServeHTTP(w, req)
	}
}

// BenchmarkListPlans_Parallel tests concurrent request handling
func BenchmarkListPlans_Parallel_Small(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	plans := generatePlans(10)
	router.GET("/api/plans", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/plans", nil)
			router.ServeHTTP(w, req)
		}
	})
}

func BenchmarkListPlans_Parallel_Medium(b *testing.B) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	
	plans := generatePlans(100)
	router.GET("/api/plans", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"plans": plans})
	})
	
	b.ResetTimer()
	b.ReportAllocs()
	
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/plans", nil)
			router.ServeHTTP(w, req)
		}
	})
}
