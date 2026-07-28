package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestNewGuard_Defaults(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)
	if g.cardinalityLimit != defaultCardinalityLimit {
		t.Errorf("expected cardinality limit %d, got %d", defaultCardinalityLimit, g.cardinalityLimit)
	}
	if g.maxLabelsPerMetric != 5 {
		t.Errorf("expected max labels per metric 5, got %d", g.maxLabelsPerMetric)
	}
}

func TestNewGuard_NilRegistryDefaultsToDefault(t *testing.T) {
	g := NewGuard(nil)
	if g.inner != prometheus.DefaultRegisterer {
		t.Error("expected inner to be prometheus.DefaultRegisterer when nil is passed")
	}
}

func TestNewGuard_WithOptions(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg, WithCardinalityLimit(100), WithMaxLabelsPerMetric(3))
	if g.cardinalityLimit != 100 {
		t.Errorf("expected cardinality limit 100, got %d", g.cardinalityLimit)
	}
	if g.maxLabelsPerMetric != 3 {
		t.Errorf("expected max labels per metric 3, got %d", g.maxLabelsPerMetric)
	}
}

func TestNewGuard_WithOptionsZeroIgnored(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg, WithCardinalityLimit(0), WithMaxLabelsPerMetric(0))
	if g.cardinalityLimit != defaultCardinalityLimit {
		t.Errorf("expected cardinality limit %d when 0 is passed", defaultCardinalityLimit)
	}
	if g.maxLabelsPerMetric != 5 {
		t.Errorf("expected max labels per metric 5 when 0 is passed")
	}
}

func TestGuard_RegisterAllowedLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_allowed_labels",
		Help: "A test counter with allowed labels",
	}, []string{"route", "method", "status"})

	err := g.Register(cv)
	if err != nil {
		t.Fatalf("expected no error for allowed labels, got: %v", err)
	}

	count := testutil.CollectAndCount(cv)
	if count < 0 {
		t.Error("expected collectable metric")
	}
}

func TestGuard_RegisterBlockedLabel(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	tests := []struct {
		name      string
		labelName string
	}{
		{"raw_path blocked", "raw_path"},
		{"user_id blocked", "user_id"},
		{"customer_id blocked", "customer_id"},
		{"email blocked", "email"},
		{"ip_address blocked", "ip_address"},
		{"session_id blocked", "session_id"},
		{"request_id blocked", "request_id"},
		{"trace_id blocked", "trace_id"},
		{"span_id blocked", "span_id"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv := prometheus.NewCounterVec(prometheus.CounterOpts{
				Name: "test_blocked",
				Help: "Should be rejected",
			}, []string{tt.labelName})

			err := g.Register(cv)
			if err == nil {
				t.Fatal("expected error for blocked label, got nil")
			}
			if !blockedLabelValues[tt.labelName] {
				t.Errorf("label %q was not in blocked set", tt.labelName)
			}
		})
	}
}

func TestGuard_RegisterTooManyLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg, WithMaxLabelsPerMetric(3))

	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_too_many_labels",
		Help: "Should be rejected",
	}, []string{"a", "b", "c", "d"})

	err := g.Register(cv)
	if err == nil {
		t.Fatal("expected error for too many labels, got nil")
	}
}

func TestGuard_RegisterTooManyLabelsDefaultLimit(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_exceeds_default",
		Help: "Should be rejected",
	}, []string{"a", "b", "c", "d", "e", "f"})

	err := g.Register(cv)
	if err == nil {
		t.Fatal("expected error for exceeding default 5 label limit, got nil")
	}
}

func TestGuard_RegisterHistogramVec(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	hv := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "test_histogram",
		Help:    "A test histogram",
		Buckets: prometheus.DefBuckets,
	}, []string{"status"})

	err := g.Register(hv)
	if err != nil {
		t.Fatalf("expected no error for allowed histogram, got: %v", err)
	}
}

func TestGuard_RegisterGaugeVec(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	gv := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "test_gauge",
		Help: "A test gauge",
	}, []string{"datacenter"})

	err := g.Register(gv)
	if err != nil {
		t.Fatalf("expected no error for allowed gauge, got: %v", err)
	}
}

func TestGuard_RegisterNoLabels(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	h := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_nolabel_histogram",
		Help:    "A histogram with no labels",
		Buckets: prometheus.DefBuckets,
	})

	err := g.Register(h)
	if err != nil {
		t.Fatalf("expected no error for label-less metric, got: %v", err)
	}
}

func TestGuard_MustRegisterPanicsOnBlocked(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected MustRegister to panic on blocked label")
		}
	}()

	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_must_register_panic",
		Help: "Should panic",
	}, []string{"user_id"})

	g.MustRegister(cv)
}

func TestGuard_MustRegisterSuccess(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("MustRegister panicked unexpectedly: %v", r)
		}
	}()

	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_must_register_ok",
		Help: "Should not panic",
	}, []string{"route"})

	g.MustRegister(cv)
}

func TestGuard_AddLabelCombination(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg, WithCardinalityLimit(3))

	for i := 0; i < 3; i++ {
		if err := g.AddLabelCombination("test_metric"); err != nil {
			t.Fatalf("unexpected error on combo %d: %v", i, err)
		}
	}

	if err := g.AddLabelCombination("test_metric"); err == nil {
		t.Fatal("expected error when exceeding cardinality limit")
	}
}

func TestGuard_AddLabelCombinationUnlimited(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	for i := 0; i < 100; i++ {
		if err := g.AddLabelCombination("test_unlimited"); err != nil {
			t.Fatalf("unexpected error on combo %d: %v", i, err)
		}
	}

	g.mu.RLock()
	count := g.observedComboCount["test_unlimited"]
	g.mu.RUnlock()
	if count != 100 {
		t.Errorf("expected 100 combos, got %d", count)
	}
}

func TestGuard_CheckLabelCombination(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg, WithCardinalityLimit(2))

	g.AddLabelCombination("test_check")
	g.AddLabelCombination("test_check")

	if err := g.CheckLabelCombination("test_check"); err == nil {
		t.Fatal("expected error when at cardinality limit")
	}

	if err := g.CheckLabelCombination("unknown"); err != nil {
		t.Fatalf("unexpected error for unseen metric: %v", err)
	}
}

func TestGuard_Unregister(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_unregister",
		Help: "Will be unregistered",
	}, []string{"route"})

	if err := g.Register(cv); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	g.AddLabelCombination("test_unregister")

	if !g.Unregister(cv) {
		t.Fatal("expected Unregister to return true")
	}

	g.mu.RLock()
	_, exists := g.observedComboCount["test_unregister"]
	g.mu.RUnlock()
	if exists {
		t.Error("expected combo count to be removed after unregister")
	}
}

func TestGuard_NewCounterVec(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	cv, err := g.NewCounterVec(prometheus.CounterOpts{
		Name: "guard_new_counter",
		Help: "Created via NewCounterVec",
	}, []string{"method"})
	if err != nil {
		t.Fatalf("NewCounterVec failed: %v", err)
	}
	if cv == nil {
		t.Fatal("expected non-nil CounterVec")
	}
}

func TestGuard_NewCounterVecBlockedLabel(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	_, err := g.NewCounterVec(prometheus.CounterOpts{
		Name: "guard_new_counter_blocked",
		Help: "Should be rejected",
	}, []string{"user_id"})
	if err == nil {
		t.Fatal("expected error for blocked label")
	}
}

func TestGuard_NewGaugeVec(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	gv, err := g.NewGaugeVec(prometheus.GaugeOpts{
		Name: "guard_new_gauge",
		Help: "Created via NewGaugeVec",
	}, []string{"datacenter"})
	if err != nil {
		t.Fatalf("NewGaugeVec failed: %v", err)
	}
	if gv == nil {
		t.Fatal("expected non-nil GaugeVec")
	}
}

func TestGuard_NewHistogramVec(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	hv, err := g.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "guard_new_histogram",
		Help:    "Created via NewHistogramVec",
		Buckets: prometheus.DefBuckets,
	}, []string{"status"})
	if err != nil {
		t.Fatalf("NewHistogramVec failed: %v", err)
	}
	if hv == nil {
		t.Fatal("expected non-nil HistogramVec")
	}
}

func TestGuard_AlreadyRegisteredError(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_already_registered",
		Help: "A test counter",
	}, []string{"route"})

	if err := g.Register(cv); err != nil {
		t.Fatalf("first registration failed: %v", err)
	}

	err := g.Register(cv)
	if err == nil {
		t.Fatal("expected AlreadyRegisteredError on duplicate registration")
	}

	var are prometheus.AlreadyRegisteredError
	if !asAlreadyRegisteredError(err, &are) {
		t.Fatalf("expected AlreadyRegisteredError, got %T: %v", err, err)
	}
	if are.ExistingCollector == nil {
		t.Fatal("existing collector should not be nil in AlreadyRegisteredError")
	}
}

func asAlreadyRegisteredError(err error, target *prometheus.AlreadyRegisteredError) bool {
	if err == nil {
		return false
	}
	are, ok := err.(prometheus.AlreadyRegisteredError)
	if !ok {
		return false
	}
	*target = are
	return true
}

func TestFQLabels_NoLabels(t *testing.T) {
	h := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_nolabels",
		Help:    "No labels",
		Buckets: prometheus.DefBuckets,
	})
	desc, err := collectorDesc(h)
	if err != nil {
		t.Fatalf("collectorDesc failed: %v", err)
	}
	labels := fqLabels(desc)
	if labels != nil {
		t.Errorf("expected nil for label-less metric, got %v", labels)
	}
}

func TestFQLabels_WithLabels(t *testing.T) {
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_fq",
		Help: "Has labels",
	}, []string{"route", "method"})
	desc, err := collectorDesc(cv)
	if err != nil {
		t.Fatalf("collectorDesc failed: %v", err)
	}
	labels := fqLabels(desc)
	if len(labels) != 2 {
		t.Fatalf("expected 2 labels, got %d: %v", len(labels), labels)
	}
	if labels[0] != "route" {
		t.Errorf("expected labels[0]='route', got %q", labels[0])
	}
	if labels[1] != "method" {
		t.Errorf("expected labels[1]='method', got %q", labels[1])
	}
}

func TestNameFromDesc(t *testing.T) {
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_name_extraction",
		Help: "Extract this name",
	}, []string{"route"})
	desc, err := collectorDesc(cv)
	if err != nil {
		t.Fatalf("collectorDesc failed: %v", err)
	}
	name := nameFromDesc(desc)
	if name != "test_name_extraction" {
		t.Errorf("expected 'test_name_extraction', got %q", name)
	}
}

func TestNameFromDesc_NoLabels(t *testing.T) {
	h := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "test_name_no_labels",
		Help:    "No labels",
		Buckets: prometheus.DefBuckets,
	})
	desc, err := collectorDesc(h)
	if err != nil {
		t.Fatalf("collectorDesc failed: %v", err)
	}
	name := nameFromDesc(desc)
	if name != "test_name_no_labels" {
		t.Errorf("expected 'test_name_no_labels', got %q", name)
	}
}

func TestGuard_RegisterAndObserve(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := NewGuard(reg)

	cv := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test_observe_counter",
		Help: "A counter to observe",
	}, []string{"status"})

	if err := g.Register(cv); err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// Observe with different label values.
	cv.WithLabelValues("200").Inc()
	cv.WithLabelValues("404").Inc()
	cv.WithLabelValues("500").Inc()

	if val := testutil.ToFloat64(cv.WithLabelValues("200")); val != 1 {
		t.Errorf("expected 1 for status=200, got %f", val)
	}
	if val := testutil.ToFloat64(cv.WithLabelValues("404")); val != 1 {
		t.Errorf("expected 1 for status=404, got %f", val)
	}
	if val := testutil.ToFloat64(cv.WithLabelValues("500")); val != 1 {
		t.Errorf("expected 1 for status=500, got %f", val)
	}
}
