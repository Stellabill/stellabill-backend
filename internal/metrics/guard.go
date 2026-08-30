package metrics

import (
	"fmt"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// defaultCardinalityLimit is the maximum number of distinct label-value
// combinations a metric family may observe before new combinations are rejected.
const defaultCardinalityLimit = 500

// blockedLabelValues contains label names whose values typically derive from
// unbounded or user-controlled input (raw path, user_id, etc.). Metrics
// declaring any of these labels will be rejected at registration.
var blockedLabelValues = map[string]bool{
	"raw_path":     true,
	"user_id":      true,
	"customer_id":  true,
	"subscriber":   true,
	"email":        true,
	"ip":           true,
	"ip_address":   true,
	"session_id":   true,
	"request_id":   true,
	"trace_id":     true,
	"span_id":      true,
	"bearer_token": true,
}

// Guard wraps a prometheus.Registerer and enforces cardinality-safety rules
// at metric registration and, for vec-metrics, at observation time.
//
// Registration-time checks:
//   - Reject label names present in blockedLabelValues.
//   - Reject more than maxLabelsPerMetric label dimensions.
//
// Observation-time checks (vec metrics):
//   - Each (metric, label-values) pair is tracked; the limit is enforced per metric.
type Guard struct {
	inner prometheus.Registerer

	mu                 sync.RWMutex
	observedComboCount map[string]int // metric name -> count of distinct label-value tuples seen
	cardinalityLimit   int
	maxLabelsPerMetric int
}

// GuardOption configures a Guard.
type GuardOption func(*Guard)

// WithCardinalityLimit sets the maximum number of distinct label-value
// combinations a single metric may observe (default 500).
func WithCardinalityLimit(limit int) GuardOption {
	return func(g *Guard) {
		if limit > 0 {
			g.cardinalityLimit = limit
		}
	}
}

// WithMaxLabelsPerMetric sets the maximum number of label dimensions a
// metric may declare (default 5).
func WithMaxLabelsPerMetric(n int) GuardOption {
	return func(g *Guard) {
		if n > 0 {
			g.maxLabelsPerMetric = n
		}
	}
}

// NewGuard wraps reg in a new Guard. When reg is nil, prometheus.DefaultRegisterer
// is used.
func NewGuard(reg prometheus.Registerer, opts ...GuardOption) *Guard {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	g := &Guard{
		inner:              reg,
		observedComboCount: make(map[string]int),
		cardinalityLimit:   defaultCardinalityLimit,
		maxLabelsPerMetric: 5,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Register implements prometheus.Registerer. It validates the collector's
// descriptor against the guard rules before delegating to the inner registerer.
func (g *Guard) Register(c prometheus.Collector) error {
	desc, err := collectorDesc(c)
	if err != nil {
		return err
	}
	if err := g.validateDesc(desc); err != nil {
		return err
	}
	return g.inner.Register(c)
}

// MustRegister implements prometheus.Registerer. It panics on validation
// failure or inner registration error.
func (g *Guard) MustRegister(c ...prometheus.Collector) {
	for _, col := range c {
		if err := g.Register(col); err != nil {
			panic(err)
		}
	}
}

// Unregister delegates to the inner registerer.
func (g *Guard) Unregister(c prometheus.Collector) bool {
	desc, err := collectorDesc(c)
	if err == nil {
		g.mu.Lock()
		delete(g.observedComboCount, nameFromDesc(desc))
		g.mu.Unlock()
	}
	return g.inner.Unregister(c)
}

// validateDesc checks the labels declared by a descriptor against the guard
// policy. It does NOT check runtime cardinality — that is handled by the
// vec-wrapper methods returned by NewCounterVec, NewHistogramVec, etc.
func (g *Guard) validateDesc(desc *prometheus.Desc) error {
	labels := fqLabels(desc)
	if len(labels) > g.maxLabelsPerMetric {
		return fmt.Errorf(
			"metric %q declares %d label(s); maximum allowed is %d",
			nameFromDesc(desc), len(labels), g.maxLabelsPerMetric,
		)
	}
	for _, l := range labels {
		if blockedLabelValues[strings.ToLower(l)] {
			return fmt.Errorf(
				"metric %q declares blocked label %q — label values derived from %q are unbounded and may cause high cardinality",
				nameFromDesc(desc), l, l,
			)
		}
	}
	return nil
}

// AddLabelCombination records that a new label-value combination was observed
// for the named metric. It returns an error if the cardinality limit has been
// reached.
func (g *Guard) AddLabelCombination(metricName string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.observedComboCount[metricName]++
	if g.observedComboCount[metricName] > g.cardinalityLimit {
		return fmt.Errorf(
			"metric %q exceeded cardinality limit of %d distinct label combinations",
			metricName, g.cardinalityLimit,
		)
	}
	return nil
}

// CheckLabelCombination is a read-only variant of AddLabelCombination that
// does not increment the counter. Useful for validating before an operation.
func (g *Guard) CheckLabelCombination(metricName string) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	count := g.observedComboCount[metricName]
	if count >= g.cardinalityLimit {
		return fmt.Errorf(
			"metric %q at cardinality limit of %d distinct label combinations",
			metricName, g.cardinalityLimit,
		)
	}
	return nil
}

// -- convenience constructors for guarded vec metrics --

// NewCounterVec creates a CounterVec registered through the guard. It accepts
// the same arguments as prometheus.NewCounterVec but validates labels before
// registration.
func (g *Guard) NewCounterVec(opts prometheus.CounterOpts, labelNames []string) (*prometheus.CounterVec, error) {
	cv := prometheus.NewCounterVec(opts, labelNames)
	if err := g.Register(cv); err != nil {
		return nil, err
	}
	return cv, nil
}

// NewGaugeVec creates a GaugeVec registered through the guard.
func (g *Guard) NewGaugeVec(opts prometheus.GaugeOpts, labelNames []string) (*prometheus.GaugeVec, error) {
	gv := prometheus.NewGaugeVec(opts, labelNames)
	if err := g.Register(gv); err != nil {
		return nil, err
	}
	return gv, nil
}

// NewHistogramVec creates a HistogramVec registered through the guard.
func (g *Guard) NewHistogramVec(opts prometheus.HistogramOpts, labelNames []string) (*prometheus.HistogramVec, error) {
	hv := prometheus.NewHistogramVec(opts, labelNames)
	if err := g.Register(hv); err != nil {
		return nil, err
	}
	return hv, nil
}

// -- internal helpers --

// collectorDesc extracts the *prometheus.Desc from a Collector by calling
// Describe with a 1-buffered channel and receiving the descriptor.
func collectorDesc(c prometheus.Collector) (*prometheus.Desc, error) {
	ch := make(chan *prometheus.Desc, 1)
	c.Describe(ch)
	desc, ok := <-ch
	if !ok {
		return nil, fmt.Errorf("collector %T did not send a descriptor", c)
	}
	return desc, nil
}

// fqLabels extracts label names (const + variable) from a Desc.
func fqLabels(desc *prometheus.Desc) []string {
	raw := desc.String()
	var all []string

	all = append(all, extractLabelSet(raw, "constLabels")...)
	all = append(all, extractLabelSet(raw, "variableLabels")...)

	return all
}

// extractLabelSet parses the label names inside a section like:
//
//	variableLabels: {route,method}
//	constLabels: {env="prod"}
func extractLabelSet(raw, section string) []string {
	prefix := section + ": {"
	start := strings.Index(raw, prefix)
	if start == -1 {
		return nil
	}
	start += len(prefix)
	end := strings.Index(raw[start:], "}")
	if end == -1 {
		return nil
	}
	inner := raw[start : start+end]
	if strings.TrimSpace(inner) == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	names := make([]string, 0, len(parts))
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) > 0 && kv[0] != "" {
			names = append(names, kv[0])
		}
	}
	return names
}

// nameFromDesc extracts the metric name from a Desc.
func nameFromDesc(desc *prometheus.Desc) string {
	raw := desc.String()
	// Format: Desc{fqName: "name", ...}
	const prefix = `fqName: "`
	start := strings.Index(raw, prefix)
	if start == -1 {
		return raw
	}
	start += len(prefix)
	end := strings.Index(raw[start:], `"`)
	if end == -1 {
		return raw
	}
	return raw[start : start+end]
}
