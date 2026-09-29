package logger

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// ---- test recorder ----------------------------------------------------------

// recordingExporter captures exported records for inspection in tests.
type recordingExporter struct {
	mu      sync.Mutex
	records []sdklog.Record
}

func (e *recordingExporter) Export(_ context.Context, records []sdklog.Record) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, r := range records {
		e.records = append(e.records, r.Clone())
	}
	return nil
}

func (e *recordingExporter) Shutdown(_ context.Context) error { return nil }
func (e *recordingExporter) ForceFlush(_ context.Context) error {
	return nil
}

func (e *recordingExporter) all() []sdklog.Record {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]sdklog.Record, len(e.records))
	copy(out, e.records)
	return out
}

// newTestHandler builds an OTelHandler backed by an in-process recorder and
// a stderr buffer.  It returns the handler, the exporter (for assertions),
// and a flush function that synchronously exports buffered records.
func newTestHandler(t *testing.T, minLevel slog.Level) (*OTelHandler, *recordingExporter, func()) {
	t.Helper()

	exp := &recordingExporter{}
	proc := sdklog.NewSimpleProcessor(exp)
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc))

	buf := &bytes.Buffer{}
	h := &OTelHandler{
		logger:   provider.Logger("test"),
		provider: provider,
		minLevel: minLevel,
		stderr:   nil, // will be set below via field trick – use a write-only buf
	}
	// Redirect stderr writes to buf for inspection.
	// We use a *os.File substitute; to avoid opening a real file we leave
	// stderr as nil and test it separately via captureStderr.
	_ = buf
	h.stderr = newFakeFile(t, buf)

	flush := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = provider.ForceFlush(ctx)
	}
	return h, exp, flush
}

// fakeFile wraps a bytes.Buffer so we can assign it to the stderr *os.File field
// without touching the real os.Stderr.  We use a pipe-backed *os.File.
func newFakeFile(t *testing.T, buf *bytes.Buffer) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	// Drain the read end asynchronously so the write end never blocks.
	go func() {
		tmp := make([]byte, 4096)
		for {
			n, err2 := r.Read(tmp)
			if n > 0 {
				buf.Write(tmp[:n])
			}
			if err2 != nil {
				return
			}
		}
	}()
	return w
}

// ---- Enabled ----------------------------------------------------------------

func TestOTelHandler_Enabled(t *testing.T) {
	h, _, _ := newTestHandler(t, slog.LevelWarn)

	cases := []struct {
		level slog.Level
		want  bool
	}{
		{slog.LevelDebug, false},
		{slog.LevelInfo, false},
		{slog.LevelWarn, true},
		{slog.LevelError, true},
	}
	for _, tc := range cases {
		got := h.Enabled(context.Background(), tc.level)
		if got != tc.want {
			t.Errorf("Enabled(%v) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestOTelHandler_Enabled_DefaultMinLevel(t *testing.T) {
	h, _, _ := newTestHandler(t, slog.LevelInfo)
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected Info to be enabled at LevelInfo minimum")
	}
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Debug to be disabled at LevelInfo minimum")
	}
}

// ---- Handle basic emit ------------------------------------------------------

func TestOTelHandler_Handle_BasicEmit(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "hello otel", 0)
	if err := h.Handle(context.Background(), r); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	flush()

	records := exp.all()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	rec := records[0]
	if rec.Body().AsString() != "hello otel" {
		t.Errorf("body = %q, want %q", rec.Body().AsString(), "hello otel")
	}
	if rec.Severity() != otellog.SeverityInfo {
		t.Errorf("severity = %v, want Info", rec.Severity())
	}
}

func TestOTelHandler_Handle_AllSlogLevels(t *testing.T) {
	cases := []struct {
		slogLevel slog.Level
		want      otellog.Severity
	}{
		{slog.LevelDebug - 4, otellog.SeverityTrace},
		{slog.LevelDebug, otellog.SeverityDebug},
		{slog.LevelInfo, otellog.SeverityInfo},
		{slog.LevelWarn, otellog.SeverityWarn},
		{slog.LevelError, otellog.SeverityError},
	}
	for _, tc := range cases {
		h, exp, flush := newTestHandler(t, tc.slogLevel)
		r := slog.NewRecord(time.Now(), tc.slogLevel, "msg", 0)
		_ = h.Handle(context.Background(), r)
		flush()
		records := exp.all()
		if len(records) != 1 {
			t.Errorf("level %v: expected 1 record, got %d", tc.slogLevel, len(records))
			continue
		}
		if records[0].Severity() != tc.want {
			t.Errorf("level %v: severity = %v, want %v", tc.slogLevel, records[0].Severity(), tc.want)
		}
	}
}

// ---- Attribute handling -----------------------------------------------------

func TestOTelHandler_Handle_StringAttr(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "attrs", 0)
	r.AddAttrs(slog.String("key", "value"))
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records exported")
	}
	var found bool
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "key" && kv.Value.AsString() == "value" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("string attr 'key=value' not found in OTel record")
	}
}

func TestOTelHandler_Handle_AllAttrKinds(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "kinds", 0)
	r.AddAttrs(
		slog.Bool("b", true),
		slog.Int("i", 42),
		slog.Int64("i64", 9999),
		slog.Float64("f", 3.14),
		slog.Duration("dur", 5*time.Second),
		slog.Time("ts", time.Unix(0, 0).UTC()),
		slog.Uint64("u64", 100),
	)
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records exported")
	}
	attrKeys := map[string]bool{}
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		attrKeys[kv.Key] = true
		return true
	})
	for _, key := range []string{"b", "i", "i64", "f", "dur", "ts", "u64"} {
		if !attrKeys[key] {
			t.Errorf("attr %q missing from OTel record", key)
		}
	}
}

func TestOTelHandler_Handle_LargeUint64_Stringified(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	big := uint64(1<<63 + 1)
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "big uint", 0)
	r.AddAttrs(slog.Uint64("big", big))
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	var found bool
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "big" {
			found = true
			if kv.Value.Kind() != otellog.KindString {
				t.Errorf("large uint64 should be stringified, got kind %v", kv.Value.Kind())
			}
		}
		return true
	})
	if !found {
		t.Error("big attr not found")
	}
}

// ---- Group handling ---------------------------------------------------------

func TestOTelHandler_WithGroup(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)
	gh := h.WithGroup("grp").(*OTelHandler)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "grouped", 0)
	r.AddAttrs(slog.String("x", "1"))
	_ = gh.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	var found bool
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "grp.x" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("expected attr key 'grp.x' after WithGroup")
	}
}

func TestOTelHandler_WithGroup_EmptyName(t *testing.T) {
	h, _, _ := newTestHandler(t, slog.LevelInfo)
	h2 := h.WithGroup("")
	// Empty group name should return the same handler type unchanged.
	if h2 != slog.Handler(h) {
		// not the same pointer but must behave identically (no group prefix)
		h3 := h2.(*OTelHandler)
		if len(h3.groups) != 0 {
			t.Error("WithGroup('') should not add a group")
		}
	}
}

func TestOTelHandler_WithGroup_Nested(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)
	gh := h.WithGroup("a").WithGroup("b").(*OTelHandler)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "nested", 0)
	r.AddAttrs(slog.String("k", "v"))
	_ = gh.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	var found bool
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "a.b.k" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("expected attr key 'a.b.k' after nested WithGroup")
	}
}

// ---- WithAttrs --------------------------------------------------------------

func TestOTelHandler_WithAttrs(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)
	ah := h.WithAttrs([]slog.Attr{slog.String("svc", "billing")}).(*OTelHandler)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "with attrs", 0)
	_ = ah.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	var found bool
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "svc" && kv.Value.AsString() == "billing" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("pre-registered attr 'svc=billing' not in OTel record")
	}
}

func TestOTelHandler_WithAttrs_DoesNotMutateParent(t *testing.T) {
	h, _, _ := newTestHandler(t, slog.LevelInfo)
	_ = h.WithAttrs([]slog.Attr{slog.String("a", "1")})
	_ = h.WithAttrs([]slog.Attr{slog.String("b", "2")})
	// Parent handler must still have zero pre-KVs.
	if len(h.preKVs) != 0 {
		t.Errorf("parent handler preKVs mutated: %v", h.preKVs)
	}
}

// ---- Inline group (empty key) -----------------------------------------------

func TestOTelHandler_InlineGroup(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "inline", 0)
	// Group with empty key: attributes should be inlined.
	r.AddAttrs(slog.Group("", slog.String("inlined", "yes")))
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	var found bool
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "inlined" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("inlined group attr 'inlined' not found")
	}
}

// ---- Trace correlation -------------------------------------------------------

func TestOTelHandler_TraceCorrelation(t *testing.T) {
	// Set up a real trace span.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		t.Fatal("span context is not valid")
	}

	h, exp, flush := newTestHandler(t, slog.LevelInfo)
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "traced", 0)
	_ = h.Handle(ctx, r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records exported")
	}

	attrs := map[string]string{}
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Value.Kind() == otellog.KindString {
			attrs[kv.Key] = kv.Value.AsString()
		}
		return true
	})

	if attrs["trace_id"] != sc.TraceID().String() {
		t.Errorf("trace_id = %q, want %q", attrs["trace_id"], sc.TraceID().String())
	}
	if attrs["span_id"] != sc.SpanID().String() {
		t.Errorf("span_id = %q, want %q", attrs["span_id"], sc.SpanID().String())
	}
	if _, ok := attrs["trace_flags"]; !ok {
		t.Error("trace_flags attribute missing")
	}
}

func TestOTelHandler_NoTraceCorrelation_NoSpan(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "no span", 0)
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records exported")
	}
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "trace_id" {
			t.Error("trace_id should not be present when no span is active")
		}
		return true
	})
}

// ---- Timestamp population ---------------------------------------------------

func TestOTelHandler_Handle_TimestampSet(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	ts := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	r := slog.NewRecord(ts, slog.LevelInfo, "ts test", 0)
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	if !records[0].Timestamp().Equal(ts) {
		t.Errorf("timestamp = %v, want %v", records[0].Timestamp(), ts)
	}
}

// ---- Zero-value attr skipping -----------------------------------------------

func TestOTelHandler_SkipsZeroValueAttr(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "zero attrs", 0)
	r.AddAttrs(slog.Attr{}) // zero value
	r.AddAttrs(slog.String("real", "kept"))
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	var emptyKeyCount int
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "" {
			emptyKeyCount++
		}
		return true
	})
	if emptyKeyCount > 0 {
		t.Errorf("zero-value attrs leaked into record (%d found)", emptyKeyCount)
	}
}
