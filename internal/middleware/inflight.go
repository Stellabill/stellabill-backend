package middleware

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gopkg.in/yaml.v3"
)

var (
	// InflightCurrent measures the current number of in-flight requests per route.
	InflightCurrent = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "inflight_current",
		Help: "Current number of in-flight requests per route",
	}, []string{"route"})
)

// ConcurrencyConfig defines per-route concurrency limits and fallback behavior.
type ConcurrencyConfig struct {
	DefaultLimit int64            `yaml:"default_limit" json:"default_limit"`
	Routes       map[string]int64 `yaml:"routes" json:"routes"`
	RetryAfter   int              `yaml:"retry_after" json:"retry_after"`
	Enabled      bool             `yaml:"enabled" json:"enabled"`
}

// LoadConcurrencyConfig reads and parses concurrency limits from a YAML file.
func LoadConcurrencyConfig(path string) (*ConcurrencyConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read concurrency config file %s: %w", path, err)
	}
	return ParseConcurrencyConfig(data)
}

// ParseConcurrencyConfig parses raw YAML bytes into a ConcurrencyConfig struct with defaults.
func ParseConcurrencyConfig(data []byte) (*ConcurrencyConfig, error) {
	cfg := &ConcurrencyConfig{
		Enabled:    true,
		RetryAfter: 5,
		Routes:     make(map[string]int64),
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse concurrency config YAML: %w", err)
	}
	if cfg.Routes == nil {
		cfg.Routes = make(map[string]int64)
	}
	if cfg.RetryAfter <= 0 {
		cfg.RetryAfter = 5
	}
	return cfg, nil
}

// InflightLimiter enforces concurrency ceilings using semaphores.
type InflightLimiter struct {
	config     *ConcurrencyConfig
	semaphores map[string]chan struct{}
	mutex      sync.RWMutex
}

// NewInflightLimiter initializes an InflightLimiter with pre-created semaphores for configured routes.
func NewInflightLimiter(cfg *ConcurrencyConfig) *InflightLimiter {
	if cfg == nil {
		cfg = &ConcurrencyConfig{
			Enabled:    true,
			RetryAfter: 5,
			Routes:     make(map[string]int64),
		}
	}
	limiter := &InflightLimiter{
		config:     cfg,
		semaphores: make(map[string]chan struct{}),
	}
	for route, limit := range cfg.Routes {
		if limit > 0 {
			limiter.semaphores[route] = make(chan struct{}, limit)
		}
	}
	return limiter
}

// getSemaphore retrieves or initializes the semaphore for a given route.
// Returns (sem, true) if concurrency limiting is enforced on this route, or (nil, false) if unlimited/disabled.
func (l *InflightLimiter) getSemaphore(route string) (chan struct{}, bool) {
	if !l.config.Enabled {
		return nil, false
	}

	l.mutex.RLock()
	sem, exists := l.semaphores[route]
	limit, configExists := l.config.Routes[route]
	l.mutex.RUnlock()

	if exists {
		return sem, true
	}

	if !configExists {
		limit = l.config.DefaultLimit
	}
	if limit <= 0 {
		return nil, false
	}

	l.mutex.Lock()
	defer l.mutex.Unlock()
	sem, exists = l.semaphores[route]
	if !exists {
		sem = make(chan struct{}, limit)
		l.semaphores[route] = sem
	}
	return sem, true
}

// InflightMiddleware creates a Gin middleware that caps in-flight requests per route
// using a semaphore and sheds excess load with a 503 Service Unavailable response.
func InflightMiddleware(cfg *ConcurrencyConfig) gin.HandlerFunc {
	limiter := NewInflightLimiter(cfg)
	return InflightMiddlewareWithLimiter(limiter)
}

// InflightMiddlewareWithLimiter returns a Gin middleware using an existing InflightLimiter instance.
func InflightMiddlewareWithLimiter(limiter *InflightLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if limiter == nil || !limiter.config.Enabled {
			c.Next()
			return
		}

		route := c.FullPath()
		if route == "" {
			// Fallback for unmatched routes (e.g. 404s) or when routing hasn't completed.
			// Using a fixed label prevents Prometheus label cardinality explosion from random URI scanning.
			route = "unmatched_route"
		}

		sem, enabled := limiter.getSemaphore(route)
		if !enabled {
			c.Next()
			return
		}

		select {
		case sem <- struct{}{}:
			// Acquired semaphore token successfully
		default:
			// Concurrency limit exceeded; shed load immediately
			c.Header("Retry-After", fmt.Sprintf("%d", limiter.config.RetryAfter))
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error":   "service unavailable",
				"code":    "CONCURRENCY_LIMIT_EXCEEDED",
				"message": "Too many concurrent requests for this endpoint. Please try again later.",
			})
			c.Abort()
			return
		}

		InflightCurrent.WithLabelValues(route).Inc()

		defer func() {
			<-sem
			InflightCurrent.WithLabelValues(route).Dec()
		}()

		c.Next()
	}
}
