package tracing

import (
	"container/list"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

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
