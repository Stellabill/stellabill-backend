package tracing

import (
	"container/list"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// tailConfig holds configuration for the tail sampling processor.
type tailConfig struct {
	// enabled gates the entire tail-sampling feature. When false the pipeline
	// falls back to the head (TenantAwareSampler) decision only.
	enabled bool
	// latency is the minimum root-span duration that triggers keep.
	// Defaults to 500 ms; validated range: (0, 600_000] ms when enabled.
	latency time.Duration
	// baselineRate is the fraction of otherwise-dropped traces to keep for
	// baseline visibility. Range [0, 1]; 0 means keep nothing extra.
	baselineRate float64
	// maxTraces is the maximum number of in-flight trace buffers held in
	// memory simultaneously. Oldest is evicted when the cap is reached.
	maxTraces int
	// maxSpans is the per-trace span buffer cap.
	maxSpans int
	// decisionWindow is how long the processor waits for a root span before
	// falling back to the head-sampling decision.
	decisionWindow time.Duration
}

const (
	maxAllowedLatencyMS = 600_000 // 10 minutes, hard upper bound
)

// tailConfigFromEnv reads tail-sampling configuration from environment
// variables and returns a validated tailConfig.
//
//	TRACING_TAIL_ENABLED      "true"|"false"  (default: false; empty string → false)
//	TRACING_TAIL_LATENCY_MS   integer ms      (default: 500; range 1–600000)
//	TRACING_TAIL_ERROR_RATE   float64         (default: 0;   range [0, 1])
//	TAIL_MAX_TRACES            integer         (default: 10000)
//	TAIL_MAX_SPANS             integer         (default: 500)
//	TAIL_DECISION_WINDOW       duration string (default: 10s)
//
// Validation errors are only returned when TRACING_TAIL_ENABLED is "true" (or
// unambiguously set). An unset or empty TRACING_TAIL_ENABLED is treated as
// disabled and no further validation is performed.
func tailConfigFromEnv() (tailConfig, error) {
	cfg := tailConfig{
		enabled:        false,
		latency:        500 * time.Millisecond,
		baselineRate:   0,
		maxTraces:      10000,
		maxSpans:       500,
		decisionWindow: 10 * time.Second,
	}

	// --- TRACING_TAIL_ENABLED ---
	enabledRaw := os.Getenv("TRACING_TAIL_ENABLED")
	switch strings.ToLower(strings.TrimSpace(enabledRaw)) {
	case "", "false", "0", "no":
		// Feature is off; skip all further validation of tail knobs.
		return cfg, nil
	case "true", "1", "yes":
		cfg.enabled = true
	default:
		return tailConfig{}, fmt.Errorf(
			"TRACING_TAIL_ENABLED: invalid value %q; use \"true\" or \"false\"", enabledRaw)
	}

	// --- TRACING_TAIL_LATENCY_MS (required when enabled) ---
	if v := os.Getenv("TRACING_TAIL_LATENCY_MS"); v != "" {
		ms, err := strconv.ParseInt(v, 10, 64)
		if err != nil || ms <= 0 || ms > maxAllowedLatencyMS {
			return tailConfig{}, fmt.Errorf(
				"TRACING_TAIL_LATENCY_MS: invalid value %q; must be an integer in range [1, %d]",
				v, maxAllowedLatencyMS)
		}
		cfg.latency = time.Duration(ms) * time.Millisecond
	}

	// --- TRACING_TAIL_ERROR_RATE ---
	if v := os.Getenv("TRACING_TAIL_ERROR_RATE"); v != "" {
		rate, err := strconv.ParseFloat(v, 64)
		if err != nil || rate < 0 || rate > 1 {
			return tailConfig{}, fmt.Errorf(
				"TRACING_TAIL_ERROR_RATE: invalid value %q; must be a float in range [0, 1]", v)
		}
		cfg.baselineRate = rate
	}

	// --- Legacy / operational knobs (best-effort; malformed values are silently ignored) ---
	if v := os.Getenv("TAIL_MAX_TRACES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.maxTraces = n
		}
	}
	if v := os.Getenv("TAIL_MAX_SPANS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.maxSpans = n
		}
	}
	if v := os.Getenv("TAIL_DECISION_WINDOW"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			cfg.decisionWindow = d
		}
	}
	// TAIL_LATENCY_THRESHOLD is superseded by TRACING_TAIL_LATENCY_MS; kept
	// for backward compatibility when the new env var is absent.
	if os.Getenv("TRACING_TAIL_LATENCY_MS") == "" {
		if v := os.Getenv("TAIL_LATENCY_THRESHOLD"); v != "" {
			if d, err := time.ParseDuration(v); err == nil && d > 0 {
				cfg.latency = d
			}
		}
	}

	return cfg, nil
}

const (
	headSampledAttribute  = "tracing.tail.head_sampled"
	headSampledTraceState = "sttail"
)

// tailSampler records every span temporarily and stores the delegate's
// original head decision as an internal attribute. The export decision is
// made by tailSpanProcessor after the request root has ended.
type tailSampler struct {
	delegate sdktrace.Sampler
}

func newTailSampler(delegate sdktrace.Sampler) sdktrace.Sampler {
	return tailSampler{delegate: delegate}
}

func (s tailSampler) ShouldSample(parameters sdktrace.SamplingParameters) sdktrace.SamplingResult {
	parent := trace.SpanContextFromContext(parameters.ParentContext)
	inheritedDecision := parent.TraceState().Get(headSampledTraceState)

	result := s.delegate.ShouldSample(parameters)
	headSampled := result.Decision == sdktrace.RecordAndSample
	if inheritedDecision != "" {
		headSampled = inheritedDecision == "1"
	}
	result.Decision = sdktrace.RecordAndSample
	result.Attributes = append(result.Attributes,
		attribute.Bool(headSampledAttribute, headSampled),
	)
	value := "0"
	if headSampled {
		value = "1"
	}
	if traceState, err := result.Tracestate.Insert(headSampledTraceState, value); err == nil {
		result.Tracestate = traceState
	}
	return result
}

func (s tailSampler) Description() string {
	return fmt.Sprintf("TailRecording{%s}", s.delegate.Description())
}

type bufferedTrace struct {
	spans     []sdktrace.ReadOnlySpan
	firstSeen time.Time
	order     *list.Element
}

type traceDecision struct {
	keep      bool
	expiresAt time.Time
}

// tailSpanProcessor bounds memory by trace count and spans per trace. It
// delegates retained spans to the normal batch processor and never performs
// exporter I/O while holding its lock.
type tailSpanProcessor struct {
	next sdktrace.SpanProcessor
	cfg  tailConfig

	mu        sync.Mutex
	traces    map[trace.TraceID]*bufferedTrace
	decisions map[trace.TraceID]traceDecision
	order     *list.List
	stopped   bool
	stop      chan struct{}
	done      chan struct{}
}

func newTailSpanProcessor(next sdktrace.SpanProcessor, cfg tailConfig) *tailSpanProcessor {
	p := &tailSpanProcessor{
		next:      next,
		cfg:       cfg,
		traces:    make(map[trace.TraceID]*bufferedTrace),
		decisions: make(map[trace.TraceID]traceDecision),
		order:     list.New(),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go p.expireLoop()
	return p
}

func (p *tailSpanProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

func (p *tailSpanProcessor) OnEnd(span sdktrace.ReadOnlySpan) {
	now := time.Now()
	traceID := span.SpanContext().TraceID()

	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	if decision, ok := p.decisions[traceID]; ok {
		if !decision.keep && isDecisionRoot(span) && p.shouldKeep(span) {
			p.addDecisionLocked(traceID, true, now)
			p.mu.Unlock()
			p.next.OnEnd(span)
			return
		}
		p.mu.Unlock()
		if decision.keep {
			p.next.OnEnd(span)
		}
		return
	}

	buffer := p.traces[traceID]
	if buffer == nil {
		if len(p.traces) >= p.cfg.maxTraces {
			p.evictOldestLocked()
		}
		buffer = &bufferedTrace{firstSeen: now}
		buffer.order = p.order.PushBack(traceID)
		p.traces[traceID] = buffer
	}
	decisionRoot := isDecisionRoot(span)
	if len(buffer.spans) < p.cfg.maxSpans {
		buffer.spans = append(buffer.spans, span)
	} else if decisionRoot {
		// The root carries the decision signals and must not be lost when a
		// trace reaches its per-trace span cap.
		buffer.spans[len(buffer.spans)-1] = span
	}

	if !decisionRoot {
		p.mu.Unlock()
		return
	}

	keep := p.shouldKeep(span)
	spans := buffer.spans
	p.removeTraceLocked(traceID)
	p.addDecisionLocked(traceID, keep, now)
	p.mu.Unlock()

	if keep {
		p.forward(spans)
	}
}

func (p *tailSpanProcessor) ForceFlush(ctx context.Context) error {
	p.expire(time.Now(), true)
	return p.next.ForceFlush(ctx)
}

func (p *tailSpanProcessor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.stopped {
		p.stopped = true
		close(p.stop)
	}
	p.mu.Unlock()

	select {
	case <-p.done:
	case <-ctx.Done():
		return ctx.Err()
	}

	// Preserve head-sampled traces during shutdown; incomplete promoted traces
	// have no root outcome and are deliberately discarded.
	p.expire(time.Now(), true)
	return p.next.Shutdown(ctx)
}

func (p *tailSpanProcessor) expireLoop() {
	interval := p.cfg.decisionWindow / 2
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(p.done)
	for {
		select {
		case now := <-ticker.C:
			p.expire(now, false)
		case <-p.stop:
			return
		}
	}
}

func (p *tailSpanProcessor) expire(now time.Time, all bool) {
	var retained []sdktrace.ReadOnlySpan

	p.mu.Lock()
	for traceID, buffer := range p.traces {
		if !all && now.Sub(buffer.firstSeen) < p.cfg.decisionWindow {
			continue
		}
		if headSampled(buffer.spans) {
			retained = append(retained, buffer.spans...)
			p.addDecisionLocked(traceID, true, now)
		} else {
			p.addDecisionLocked(traceID, false, now)
		}
		p.removeTraceLocked(traceID)
	}
	for traceID, decision := range p.decisions {
		if all || !decision.expiresAt.After(now) {
			delete(p.decisions, traceID)
		}
	}
	p.mu.Unlock()

	p.forward(retained)
}

func (p *tailSpanProcessor) evictOldestLocked() {
	oldest := p.order.Front()
	if oldest != nil {
		traceID := oldest.Value.(trace.TraceID)
		p.removeTraceLocked(traceID)
		p.addDecisionLocked(traceID, false, time.Now())
	}
}

func (p *tailSpanProcessor) removeTraceLocked(traceID trace.TraceID) {
	if buffer := p.traces[traceID]; buffer != nil {
		p.order.Remove(buffer.order)
		delete(p.traces, traceID)
	}
}

func (p *tailSpanProcessor) addDecisionLocked(traceID trace.TraceID, keep bool, now time.Time) {
	// Decision caching handles spans which end after their root. Bound it as
	// strictly as the trace buffer so high-cardinality trace IDs cannot grow
	// memory without limit.
	if _, exists := p.decisions[traceID]; !exists && len(p.decisions) >= p.cfg.maxTraces {
		for candidate := range p.decisions {
			delete(p.decisions, candidate)
			break
		}
	}
	p.decisions[traceID] = traceDecision{
		keep:      keep,
		expiresAt: now.Add(p.cfg.decisionWindow),
	}
}

func (p *tailSpanProcessor) shouldKeep(root sdktrace.ReadOnlySpan) bool {
	if headSampled([]sdktrace.ReadOnlySpan{root}) {
		return true
	}
	if root.EndTime().Sub(root.StartTime()) >= p.cfg.latency {
		return true
	}
	if root.Status().Code == codes.Error || hasErrorSignal(root) {
		return true
	}
	for _, attr := range root.Attributes() {
		switch string(attr.Key) {
		case "http.response.status_code", "http.status_code":
			if attr.Value.Type() == attribute.INT64 && attr.Value.AsInt64() >= 500 {
				return true
			}
		}
	}
	return false
}

func (p *tailSpanProcessor) forward(spans []sdktrace.ReadOnlySpan) {
	for _, span := range spans {
		p.next.OnEnd(span)
	}
}

func isDecisionRoot(span sdktrace.ReadOnlySpan) bool {
	return !span.Parent().IsValid() || span.SpanKind() == trace.SpanKindServer
}

func headSampled(spans []sdktrace.ReadOnlySpan) bool {
	for _, span := range spans {
		for _, attr := range span.Attributes() {
			if string(attr.Key) == headSampledAttribute &&
				attr.Value.Type() == attribute.BOOL && attr.Value.AsBool() {
				return true
			}
		}
	}
	return false
}

func hasErrorSignal(span sdktrace.ReadOnlySpan) bool {
	for _, attr := range span.Attributes() {
		key := strings.ToLower(string(attr.Key))
		if key == "error" || key == "error.type" || strings.HasPrefix(key, "error.") {
			switch attr.Value.Type() {
			case attribute.BOOL:
				if attr.Value.AsBool() {
					return true
				}
			case attribute.STRING:
				if attr.Value.AsString() != "" {
					return true
				}
			default:
				return true
			}
		}
	}
	for _, event := range span.Events() {
		if event.Name == "exception" {
			return true
		}
	}
	return false
}
