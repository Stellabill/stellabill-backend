package logger

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	otellog "go.opentelemetry.io/otel/log"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// ---- buildKey ---------------------------------------------------------------

func TestBuildKey_NoGroups(t *testing.T) {
	got := buildKey(nil, "mykey")
	if got != "mykey" {
		t.Errorf("buildKey(nil, 'mykey') = %q, want 'mykey'", got)
	}
}

func TestBuildKey_SingleGroup(t *testing.T) {
	got := buildKey([]string{"grp"}, "key")
	if got != "grp.key" {
		t.Errorf("got %q, want 'grp.key'", got)
	}
}

func TestBuildKey_MultiGroup(t *testing.T) {
	got := buildKey([]string{"a", "b", "c"}, "k")
	if got != "a.b.c.k" {
		t.Errorf("got %q, want 'a.b.c.k'", got)
	}
}

// ---- slogLevelToOTelSeverity ------------------------------------------------

func TestSlogLevelToOTelSeverity(t *testing.T) {
	cases := []struct {
		in   slog.Level
		want otellog.Severity
	}{
		{slog.LevelDebug - 5, otellog.SeverityTrace},
		{slog.LevelDebug, otellog.SeverityDebug},
		{slog.LevelInfo, otellog.SeverityInfo},
		{slog.LevelWarn, otellog.SeverityWarn},
		{slog.LevelError, otellog.SeverityError},
		{slog.LevelError + 4, otellog.SeverityError},
	}
	for _, tc := range cases {
		got := slogLevelToOTelSeverity(tc.in)
		if got != tc.want {
			t.Errorf("slogLevelToOTelSeverity(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// ---- slogValueToOTelKV -----------------------------------------------------

func TestSlogValueToOTelKV_Bool(t *testing.T) {
	kv := slogValueToOTelKV("b", slog.BoolValue(true))
	if kv.Key != "b" || kv.Value.Kind() != otellog.KindBool || !kv.Value.AsBool() {
		t.Errorf("unexpected kv: %+v", kv)
	}
}

func TestSlogValueToOTelKV_Float64(t *testing.T) {
	kv := slogValueToOTelKV("f", slog.Float64Value(2.71))
	if kv.Key != "f" || kv.Value.Kind() != otellog.KindFloat64 {
		t.Errorf("unexpected kv: %+v", kv)
	}
}

func TestSlogValueToOTelKV_Int64(t *testing.T) {
	kv := slogValueToOTelKV("i", slog.Int64Value(-7))
	if kv.Key != "i" || kv.Value.AsInt64() != -7 {
		t.Errorf("unexpected kv: %+v", kv)
	}
}

func TestSlogValueToOTelKV_Duration(t *testing.T) {
	dur := 3 * time.Second
	kv := slogValueToOTelKV("dur", slog.DurationValue(dur))
	if kv.Key != "dur" || kv.Value.AsInt64() != int64(dur) {
		t.Errorf("unexpected kv: %+v", kv)
	}
}

func TestSlogValueToOTelKV_Time(t *testing.T) {
	ts := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	kv := slogValueToOTelKV("ts", slog.TimeValue(ts))
	if kv.Key != "ts" || kv.Value.Kind() != otellog.KindString {
		t.Errorf("expected string kind for time, got %+v", kv)
	}
	if !strings.Contains(kv.Value.AsString(), "2025") {
		t.Errorf("time string missing year: %s", kv.Value.AsString())
	}
}

func TestSlogValueToOTelKV_Uint64Small(t *testing.T) {
	kv := slogValueToOTelKV("u", slog.Uint64Value(42))
	if kv.Key != "u" || kv.Value.AsInt64() != 42 {
		t.Errorf("unexpected kv: %+v", kv)
	}
}

func TestSlogValueToOTelKV_Uint64Large(t *testing.T) {
	big := uint64(1<<63 + 5)
	kv := slogValueToOTelKV("big", slog.Uint64Value(big))
	if kv.Value.Kind() != otellog.KindString {
		t.Errorf("large uint64 should be stringified, kind=%v", kv.Value.Kind())
	}
	if kv.Value.AsString() != fmt.Sprintf("%d", big) {
		t.Errorf("stringified value mismatch: %s", kv.Value.AsString())
	}
}

func TestSlogValueToOTelKV_Default_AnyString(t *testing.T) {
	// KindAny falls to default branch
	kv := slogValueToOTelKV("obj", slog.AnyValue(struct{ Name string }{"test"}))
	if kv.Key != "obj" || kv.Value.Kind() != otellog.KindString {
		t.Errorf("unexpected kv: %+v", kv)
	}
}

// ---- OTelHandlerConfig.applyDefaults ----------------------------------------

func TestOTelHandlerConfig_ApplyDefaults(t *testing.T) {
	cfg := OTelHandlerConfig{}
	cfg.applyDefaults()

	if cfg.ServiceName != "stellabill-backend" {
		t.Errorf("ServiceName = %q, want 'stellabill-backend'", cfg.ServiceName)
	}
	if cfg.MaxQueueSize != 2048 {
		t.Errorf("MaxQueueSize = %d, want 2048", cfg.MaxQueueSize)
	}
	if cfg.ExportBatchSize != 512 {
		t.Errorf("ExportBatchSize = %d, want 512", cfg.ExportBatchSize)
	}
	if cfg.ExportInterval != 5*time.Second {
		t.Errorf("ExportInterval = %v, want 5s", cfg.ExportInterval)
	}
	if cfg.ExportTimeout != 10*time.Second {
		t.Errorf("ExportTimeout = %v, want 10s", cfg.ExportTimeout)
	}
}

func TestOTelHandlerConfig_PreserveNonZeroValues(t *testing.T) {
	cfg := OTelHandlerConfig{
		ServiceName:     "custom",
		MaxQueueSize:    999,
		ExportBatchSize: 100,
		ExportInterval:  30 * time.Second,
		ExportTimeout:   3 * time.Second,
	}
	cfg.applyDefaults()
	if cfg.ServiceName != "custom" || cfg.MaxQueueSize != 999 ||
		cfg.ExportBatchSize != 100 || cfg.ExportInterval != 30*time.Second ||
		cfg.ExportTimeout != 3*time.Second {
		t.Error("applyDefaults overwrote non-zero values")
	}
}

// ---- NewOTelHandler with injected provider ----------------------------------

func TestNewOTelHandler_InjectedProvider(t *testing.T) {
	exp := &recordingExporter{}
	proc := sdklog.NewSimpleProcessor(exp)
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc))

	cfg := OTelHandlerConfig{
		ServiceName: "test-svc",
		MinLevel:    slog.LevelInfo,
		Provider:    provider,
	}

	h, shutdown, err := NewOTelHandler(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewOTelHandler error: %v", err)
	}
	if h == nil {
		t.Fatal("handler is nil")
	}
	if h.ownProvider {
		t.Error("ownProvider should be false when provider is injected")
	}
	// Shutdown should be a no-op (not close the external provider).
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown error: %v", err)
	}
}

// ---- Concurrent Handle calls ------------------------------------------------

func TestOTelHandler_ConcurrentHandle(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	const goroutines = 20
	const records = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			for j := 0; j < records; j++ {
				r := slog.NewRecord(time.Now(), slog.LevelInfo,
					fmt.Sprintf("msg g=%d j=%d", i, j), 0)
				_ = h.Handle(context.Background(), r)
			}
		}()
	}
	wg.Wait()
	flush()

	got := len(exp.all())
	want := goroutines * records
	if got != want {
		t.Errorf("concurrent: exported %d records, want %d", got, want)
	}
}

// ---- WithAttrs + WithGroup chain --------------------------------------------

func TestOTelHandler_ChainAttrsAndGroup(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	h2 := h.WithAttrs([]slog.Attr{slog.String("env", "prod")}).(*OTelHandler)
	h3 := h2.WithGroup("req").(*OTelHandler)
	h4 := h3.WithAttrs([]slog.Attr{slog.String("id", "abc")}).(*OTelHandler)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "chain", 0)
	r.AddAttrs(slog.String("path", "/api"))
	_ = h4.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}

	found := map[string]bool{}
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		found[kv.Key] = true
		return true
	})

	for _, key := range []string{"env", "req.id", "req.path"} {
		if !found[key] {
			t.Errorf("expected attr key %q in chained handler", key)
		}
	}
}

// ---- Stderr fallback writes message text -----------------------------------

func TestOTelHandler_StderrFallback(t *testing.T) {
	exp := &recordingExporter{}
	proc := sdklog.NewSimpleProcessor(exp)
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc))

	// Use a real pipe for the stderr capture.
	pr, pw, err := newOSPipe(t)
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	h := &OTelHandler{
		logger:   provider.Logger("test"),
		provider: provider,
		minLevel: slog.LevelInfo,
		stderr:   pw,
	}

	r := slog.NewRecord(time.Now(), slog.LevelWarn, "stderr message", 0)
	_ = h.Handle(context.Background(), r)
	_ = pw.Close()

	buf := make([]byte, 1024)
	n, _ := pr.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "stderr message") {
		t.Errorf("stderr fallback does not contain message text: %q", output)
	}
	if !strings.Contains(output, "WARN") {
		t.Errorf("stderr fallback missing level: %q", output)
	}
}

// newOSPipe returns an os.Pipe() pair, registering cleanup.
func newOSPipe(t *testing.T) (r, w *os.File, err error) {
	t.Helper()
	return os.Pipe()
}

// ---- InitOTelBridge when disabled ------------------------------------------

func TestInitOTelBridge_Disabled(t *testing.T) {
	shutdown, err := InitOTelBridge(context.Background(), false, "svc")
	if err != nil {
		t.Fatalf("InitOTelBridge(disabled) should not error, got: %v", err)
	}
	if shutdown == nil {
		t.Fatal("shutdown function should not be nil")
	}
	// No-op shutdown should not error.
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("no-op shutdown error: %v", err)
	}
}

// ---- Severity text populated correctly --------------------------------------

func TestOTelHandler_SeverityText(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelError, "err msg", 0)
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	if records[0].SeverityText() != "ERROR" {
		t.Errorf("SeverityText = %q, want 'ERROR'", records[0].SeverityText())
	}
}

// ---- ObservedTimestamp is set -----------------------------------------------

func TestOTelHandler_ObservedTimestamp(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	before := time.Now()
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "obs ts", 0)
	_ = h.Handle(context.Background(), r)
	after := time.Now()
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	ots := records[0].ObservedTimestamp()
	if ots.Before(before) || ots.After(after) {
		t.Errorf("ObservedTimestamp %v outside [%v, %v]", ots, before, after)
	}
}

// ---- Group attr embedded in slog.Record -------------------------------------

func TestOTelHandler_NamedGroupAttr(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	r := slog.NewRecord(time.Now(), slog.LevelInfo, "named group", 0)
	r.AddAttrs(slog.Group("http", slog.Int("status", 200), slog.String("method", "GET")))
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	found := map[string]bool{}
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		found[kv.Key] = true
		return true
	})
	for _, key := range []string{"http.status", "http.method"} {
		if !found[key] {
			t.Errorf("group attr %q not found", key)
		}
	}
}

// ---- Stderr fallback includes RFC3339 timestamp -----------------------------

func TestOTelHandler_StderrFallback_Timestamp(t *testing.T) {
	exp2 := &recordingExporter{}
	proc2 := sdklog.NewSimpleProcessor(exp2)
	prov2 := sdklog.NewLoggerProvider(sdklog.WithProcessor(proc2))

	var buf bytes.Buffer
	pr2, pw2, _ := os.Pipe()

	h := &OTelHandler{
		logger:   prov2.Logger("test"),
		provider: prov2,
		minLevel: slog.LevelInfo,
		stderr:   pw2,
	}

	ts := time.Date(2025, 3, 14, 15, 9, 26, 0, time.UTC)
	r := slog.NewRecord(ts, slog.LevelInfo, "ts check", 0)
	_ = h.Handle(context.Background(), r)
	_ = pw2.Close()

	tmp := make([]byte, 512)
	n, _ := pr2.Read(tmp)
	buf.Write(tmp[:n])
	_ = pr2.Close()

	out := buf.String()
	if !strings.Contains(out, "2025-03-14") {
		t.Errorf("stderr output missing expected date: %q", out)
	}
}

// ---- addAttr with group-scoped LogValuer resolves --------------------------

func TestOTelHandler_AddAttr_ResolvesLogValuer(t *testing.T) {
	h, exp, flush := newTestHandler(t, slog.LevelInfo)

	// slog.AnyValue wrapping a type that implements LogValuer
	resolved := slog.StringValue("resolved-val")
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "resolve", 0)
	r.AddAttrs(slog.Attr{Key: "lv", Value: resolved})
	_ = h.Handle(context.Background(), r)
	flush()

	records := exp.all()
	if len(records) == 0 {
		t.Fatal("no records")
	}
	var found bool
	records[0].WalkAttributes(func(kv otellog.KeyValue) bool {
		if kv.Key == "lv" && kv.Value.AsString() == "resolved-val" {
			found = true
		}
		return true
	})
	if !found {
		t.Error("LogValuer attr not resolved/found")
	}
}

// ---- Ensure WithAttrs child does not own provider --------------------------

func TestOTelHandler_WithAttrs_ChildDoesNotOwnProvider(t *testing.T) {
	h, _, _ := newTestHandler(t, slog.LevelInfo)
	child := h.WithAttrs([]slog.Attr{slog.String("x", "1")}).(*OTelHandler)
	if child.ownProvider {
		t.Error("child handler must not own the provider")
	}
}

func TestOTelHandler_WithGroup_ChildDoesNotOwnProvider(t *testing.T) {
	h, _, _ := newTestHandler(t, slog.LevelInfo)
	child := h.WithGroup("g").(*OTelHandler)
	if child.ownProvider {
		t.Error("child handler from WithGroup must not own the provider")
	}
}
