package metrics

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/trace"
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

	// DBPoolMetrics tracks database pool statistics
	DBPoolMetrics = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "db_pool_stats",
			Help: "Database pool statistics",
		},
		[]string{"stat"},
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

		observer := HTTPRequestDuration.WithLabelValues(safeRoute, safeMethod, safeStatus)
		if exemplar := spanExemplar(c.Request.Context()); exemplar != nil {
			if oe, ok := observer.(prometheus.ExemplarObserver); ok {
				oe.ObserveWithExemplar(duration, exemplar)
				HTTPRequestTotal.WithLabelValues(safeRoute, safeMethod, safeStatus).Inc()
				return
			}
		}
		observer.Observe(duration)
		HTTPRequestTotal.WithLabelValues(safeRoute, safeMethod, safeStatus).Inc()
	}
}

// spanExemplar returns a prometheus.Labels map with trace_id and span_id when
// the current span is sampled and recording. Returns nil otherwise.
func spanExemplar(ctx context.Context) prometheus.Labels {
	span := trace.SpanFromContext(ctx)
	sc := span.SpanContext()
	if !sc.IsSampled() || !span.IsRecording() {
		return nil
	}
	return prometheus.Labels{
		"trace_id": sc.TraceID().String(),
		"span_id":  sc.SpanID().String(),
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

// CacheHitsTotal tracks cache hit/miss events by layer and operation.
// layer: "redis", "inmemory", etc.
// op: "get", "set", "delete"
// result: "hit", "miss", "error"
var CacheHitsTotal = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "cache_hits_total",
		Help: "Total number of cache operations by layer, operation, and result",
	},
	[]string{"layer", "op", "result"},
)

// ShutdownDuration tracks the time taken to shut down gracefully in seconds.
var ShutdownDuration = promauto.NewHistogram(
	prometheus.HistogramOpts{
		Name:    "shutdown_duration_seconds",
		Help:    "Time taken to shut down gracefully in seconds",
		Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10, 25, 30},
	},
)

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
