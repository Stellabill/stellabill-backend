package metrics

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// HTTPRequestDuration tracks request latency by route, method, and status
	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"route", "method", "status"},
	)

	// HTTPRequestTotal tracks total requests by route, method, and status
	HTTPRequestTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"route", "method", "status"},
	)

	// DBQueryDuration tracks database query latency by operation and table
	DBQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Database query latency in seconds",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"operation", "table"},
	)

	// DBQueryTotal tracks total DB queries by operation, table, and error status
	DBQueryTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "db_queries_total",
			Help: "Total number of database queries",
		},
		[]string{"operation", "table", "error"},
	)
)



func MetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()

		route := c.FullPath()
		if route == "" {
			route = "unknown"
		}

		method := c.Request.Method

		c.Next()

		duration := time.Since(start).Seconds()
		status := strconv.Itoa(c.Writer.Status())

		safeRoute := sanitizeLabel(route)
		safeMethod := sanitizeLabel(method)
		safeStatus := sanitizeLabel(status)

		HTTPRequestDuration.WithLabelValues(safeRoute, safeMethod, safeStatus).Observe(duration)
		HTTPRequestTotal.WithLabelValues(safeRoute, safeMethod, safeStatus).Inc()
	}
}

func DBTimer(operation, table string) func(error) {
	start := time.Now()
	return func(err error) {
		duration := time.Since(start).Seconds()
		safeOp := sanitizeLabel(operation)
		safeTable := sanitizeLabel(table)

		errorLabel := "false"
		if err != nil {
			errorLabel = "true"
		}

		DBQueryDuration.WithLabelValues(safeOp, safeTable).Observe(duration)
		DBQueryTotal.WithLabelValues(safeOp, safeTable, errorLabel).Inc()
	}
}

func sanitizeLabel(value string) string {
	if value == "" {
		return "unknown"
	}
	const maxLen = 128
	if len(value) > maxLen {
		return value[:maxLen]
	}
	return value
}

func RecordDBQuery(operation, table string, duration time.Duration, err error) {
	safeOp := sanitizeLabel(operation)
	safeTable := sanitizeLabel(table)

	errorLabel := "false"
	if err != nil {
		errorLabel = "true"
	}

	DBQueryDuration.WithLabelValues(safeOp, safeTable).Observe(duration.Seconds())
	DBQueryTotal.WithLabelValues(safeOp, safeTable, errorLabel).Inc()
}
