package middleware

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	costAccumulatorContextKeyName = "cost_accumulator"
	maxTenantLabelLength          = 64
)

type costAccumulatorContextKey struct{}

// CostAccumulator tracks normalized cost units for a single request.
type CostAccumulator struct {
	mu           sync.Mutex
	dbRowsRead   int64
	externalCalls int64
	egressBytes  int64
}

// NewCostAccumulator creates a request-scoped cost accumulator.
func NewCostAccumulator() *CostAccumulator {
	return &CostAccumulator{}
}

// AddDBRowsRead records a number of database rows read for the request.
func (a *CostAccumulator) AddDBRowsRead(rows int64) {
	if rows <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.dbRowsRead += rows
}

// AddExternalCall records a number of external outbound calls for the request.
func (a *CostAccumulator) AddExternalCall(count int64) {
	if count <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.externalCalls += count
}

// AddEgressBytes records egress bytes emitted for the request.
func (a *CostAccumulator) AddEgressBytes(bytes int64) {
	if bytes <= 0 {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.egressBytes += bytes
}

// CostUnits returns the normalized unit cost for the current request.
func (a *CostAccumulator) CostUnits() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	rowUnits := a.dbRowsRead
	callUnits := a.externalCalls * 10
	byteUnits := (a.egressBytes + 1023) / 1024
	return rowUnits + callUnits + byteUnits
}

// AccumulatorFromGinContext retrieves the request cost accumulator from a gin context.
func AccumulatorFromGinContext(c *gin.Context) *CostAccumulator {
	if c == nil {
		return nil
	}
	value, exists := c.Get(costAccumulatorContextKeyName)
	if !exists {
		return nil
	}
	acc, ok := value.(*CostAccumulator)
	if !ok {
		return nil
	}
	return acc
}

// AccumulatorFromContext retrieves the request cost accumulator from a context.Context.
func AccumulatorFromContext(ctx context.Context) *CostAccumulator {
	if ctx == nil {
		return nil
	}
	value := ctx.Value(costAccumulatorContextKey{})
	acc, ok := value.(*CostAccumulator)
	if !ok {
		return nil
	}
	return acc
}

// ContextWithAccumulator attaches a request cost accumulator to a context.Context.
func ContextWithAccumulator(ctx context.Context, acc *CostAccumulator) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, costAccumulatorContextKey{}, acc)
}

// CostAccountingMiddleware captures request cost data and exposes it on the response.
func CostAccountingMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		acc := NewCostAccumulator()
		c.Set(costAccumulatorContextKeyName, acc)
		c.Request = c.Request.WithContext(ContextWithAccumulator(c.Request.Context(), acc))

		writer := &costAccountingResponseWriter{
			ResponseWriter: c.Writer,
			acc:            acc,
		}
		c.Writer = writer

		c.Next()

		costUnits := acc.CostUnits()
		c.Header("X-Cost-Units", strconv.FormatInt(costUnits, 10))

		tenantID := strings.TrimSpace(c.GetString("tenantID"))
		if tenantID == "" {
			tenantID = strings.TrimSpace(c.GetHeader("X-Tenant-ID"))
		}
		if tenantID == "" {
			tenantID = "unknown"
		}
		if len(tenantID) > maxTenantLabelLength {
			tenantID = tenantID[:maxTenantLabelLength]
		}
		TenantCostUnitsTotal.WithLabelValues(tenantID).Add(float64(costUnits))
	}
}

type costAccountingResponseWriter struct {
	gin.ResponseWriter
	bytesWritten int
	acc          *CostAccumulator
}

func (w *costAccountingResponseWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.bytesWritten += n
	if w.acc != nil && n > 0 {
		w.acc.AddEgressBytes(int64(n))
	}
	return n, err
}

func (w *costAccountingResponseWriter) WriteString(s string) (int, error) {
	n, err := w.ResponseWriter.WriteString(s)
	w.bytesWritten += n
	if w.acc != nil && n > 0 {
		w.acc.AddEgressBytes(int64(n))
	}
	return n, err
}

var (
	IdempotencyKeysPurgedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "idempotency_keys_purged_total",
		Help: "Total number of expired idempotency keys purged",
	})

	IdempotencyKeysExpiredPending = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "idempotency_keys_expired_pending",
		Help: "Number of expired idempotency keys pending deletion",
	})

	TenantCostUnitsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "tenant_cost_units_total",
		Help: "Total normalized cost units emitted per request by tenant",
	}, []string{"tenant"})
)
