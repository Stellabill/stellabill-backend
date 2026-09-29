package cdc

import (
	"context"
	"testing"
)

// TestMemorySink_BasicWriteRead verifies basic Write/Read/Close behavior.
func TestMemorySink_BasicWriteRead(t *testing.T) {
	sink := NewMemorySink()
	ctx := context.Background()

	changes := []Change{
		{Schema: "public", Table: "plans", Op: "insert", After: map[string]any{"id": "1"}},
		{Schema: "public", Table: "subscriptions", Op: "update", Before: map[string]any{"id": "2"}, After: map[string]any{"id": "2", "status": "active"}},
		{Schema: "public", Table: "statements", Op: "delete", Before: map[string]any{"id": "3"}},
	}

	for _, ch := range changes {
		if err := sink.WriteChange(ctx, ch); err != nil {
			t.Fatalf("WriteChange failed: %v", err)
		}
	}

	got := sink.Changes()
	if len(got) != 3 {
		t.Fatalf("expected 3 changes, got %d", len(got))
	}

	for i, expected := range changes {
		if got[i].Op != expected.Op || got[i].Table != expected.Table {
			t.Errorf("change %d mismatch: expected %s/%s, got %s/%s",
				i, expected.Op, expected.Table, got[i].Op, got[i].Table)
		}
	}
}

// TestMemorySink_CloseRejectsWrites verifies writes after close are rejected.
func TestMemorySink_CloseRejectsWrites(t *testing.T) {
	sink := NewMemorySink()
	ctx := context.Background()

	if err := sink.WriteChange(ctx, Change{Op: "insert", Table: "plans"}); err != nil {
		t.Fatalf("write before close failed: %v", err)
	}

	if err := sink.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}

	if err := sink.WriteChange(ctx, Change{Op: "insert", Table: "plans"}); err == nil {
		t.Fatal("expected error writing after close, got nil")
	}

	// Double close is safe.
	if err := sink.Close(); err != nil {
		t.Fatalf("second close should be safe: %v", err)
	}
}

// TestMemorySink_FlushNoop verifies Flush doesn't error.
func TestMemorySink_FlushNoop(t *testing.T) {
	sink := NewMemorySink()
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatal("Flush should be no-op")
	}
}

// TestMemorySink_ChangesReturnsCopy verifies the returned slice is a copy.
func TestMemorySink_ChangesReturnsCopy(t *testing.T) {
	sink := NewMemorySink()
	_ = sink.WriteChange(context.Background(), Change{Op: "insert", Table: "plans"})

	first := sink.Changes()
	// Mutate the returned slice
	first[0] = Change{Op: "delete", Table: "nope"}

	second := sink.Changes()
	if second[0].Op != "insert" {
		t.Fatal("Changes() did not return a defensive copy")
	}
}

// TestMemorySink_ThreadSafety verifies concurrent access is safe.
func TestMemorySink_ThreadSafety(t *testing.T) {
	sink := NewMemorySink()
	ctx := context.Background()

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(n int) {
			_ = sink.WriteChange(ctx, Change{Op: "insert", Table: "plans", After: map[string]any{"n": n}})
			_ = sink.Changes()
			done <- struct{}{}
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	if len(sink.Changes()) != 10 {
		t.Fatalf("expected 10 changes, got %d", len(sink.Changes()))
	}
}

// TestStdoutSink_WriteChange verifies stdout sink doesn't error.
func TestStdoutSink_WriteChange(t *testing.T) {
	sink := NewStdoutSink()
	ctx := context.Background()

	err := sink.WriteChange(ctx, Change{
		Schema: "public",
		Table:  "plans",
		Op:     "insert",
		After:  map[string]any{"id": "plan-1", "name": "basic"},
	})
	if err != nil {
		t.Fatalf("WriteChange failed: %v", err)
	}
}

// TestStdoutSink_FlushClose verifies flush/close are safe no-ops.
func TestStdoutSink_FlushClose(t *testing.T) {
	sink := NewStdoutSink()
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatal("Flush failed")
	}
	if err := sink.Close(); err != nil {
		t.Fatal("Close failed")
	}
	// double close
	if err := sink.Close(); err != nil {
		t.Fatal("second Close failed")
	}
}

// TestKafkaSink_BasicWrite verifies the Kafka sink with a mock writer.
func TestKafkaSink_BasicWrite(t *testing.T) {
	mw := newMockMessageWriter()
	sink, err := NewKafkaSink(KafkaSinkConfig{
		Writer:      mw,
		TopicPrefix: "stellabill",
	})
	if err != nil {
		t.Fatalf("NewKafkaSink failed: %v", err)
	}

	ctx := context.Background()
	change := Change{
		Schema: "public",
		Table:  "plans",
		Op:     "insert",
		After:  map[string]any{"id": "p1", "name": "basic"},
		LSN:    42,
	}

	if err := sink.WriteChange(ctx, change); err != nil {
		t.Fatalf("WriteChange failed: %v", err)
	}

	msgs := mw.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	if msgs[0].Topic != "stellabill.public.plans" {
		t.Errorf("expected topic 'stellabill.public.plans', got %q", msgs[0].Topic)
	}

	if len(msgs[0].Key) == 0 {
		t.Error("expected non-empty key")
	}

	if len(msgs[0].Value) == 0 {
		t.Error("expected non-empty value")
	}
}

// TestKafkaSink_TopicPrefixDefault verifies default prefix behavior.
func TestKafkaSink_TopicPrefixDefault(t *testing.T) {
	mw := newMockMessageWriter()
	sink, err := NewKafkaSink(KafkaSinkConfig{Writer: mw})
	if err != nil {
		t.Fatalf("NewKafkaSink failed: %v", err)
	}
	_ = sink.WriteChange(context.Background(), Change{Schema: "public", Table: "plans", Op: "insert", After: map[string]any{"id": "1"}})

	msgs := mw.Messages()
	if msgs[0].Topic != "stellabill.public.plans" {
		t.Errorf("expected default prefix, got %q", msgs[0].Topic)
	}
}

// TestKafkaSink_NilWriterFails verifies nil writer is rejected.
func TestKafkaSink_NilWriterFails(t *testing.T) {
	_, err := NewKafkaSink(KafkaSinkConfig{})
	if err == nil {
		t.Fatal("expected error for nil writer")
	}
}

// TestKafkaSink_CloseRejectsWrites verifies writes fail after close.
func TestKafkaSink_CloseRejectsWrites(t *testing.T) {
	mw := newMockMessageWriter()
	sink, _ := NewKafkaSink(KafkaSinkConfig{Writer: mw})

	_ = sink.Close()

	err := sink.WriteChange(context.Background(), Change{Op: "insert", Table: "plans"})
	if err == nil {
		t.Fatal("expected error writing after close")
	}
}

// TestKafkaSink_CloseIdempotent verifies multiple closes are safe.
func TestKafkaSink_CloseIdempotent(t *testing.T) {
	mw := newMockMessageWriter()
	sink, _ := NewKafkaSink(KafkaSinkConfig{Writer: mw})

	_ = sink.Close()
	_ = sink.Close()
	_ = sink.Close()
}

// TestKafkaSink_MultipleTables verifies topics are segmented by table.
func TestKafkaSink_MultipleTables(t *testing.T) {
	mw := newMockMessageWriter()
	sink, _ := NewKafkaSink(KafkaSinkConfig{Writer: mw, TopicPrefix: "cdc"})

	ctx := context.Background()

	changes := []Change{
		{Schema: "public", Table: "plans", Op: "insert", After: map[string]any{"id": "1"}},
		{Schema: "public", Table: "subscriptions", Op: "insert", After: map[string]any{"id": "2"}},
		{Schema: "public", Table: "statements", Op: "insert", After: map[string]any{"id": "3"}},
	}

	for _, ch := range changes {
		_ = sink.WriteChange(ctx, ch)
	}

	msgs := mw.Messages()
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	expectedTopics := map[string]bool{
		"cdc.public.plans":         false,
		"cdc.public.subscriptions": false,
		"cdc.public.statements":    false,
	}
	for _, msg := range msgs {
		expectedTopics[msg.Topic] = true
	}
	for topic, found := range expectedTopics {
		if !found {
			t.Errorf("missing message for topic %q", topic)
		}
	}
}

// TestValidateSink verifies the ValidateSink helper works.
func TestValidateSink(t *testing.T) {
	sink := NewMemorySink()
	ValidateSink(t, sink)
}

// TestValidateSink_Stdout verifies stdout sink passes validation.
func TestValidateSink_Stdout(t *testing.T) {
	ValidateSink(t, NewStdoutSink())
}

// TestMockMessageWriter verifies the mock writer behavior.
func TestMockMessageWriter(t *testing.T) {
	mw := newMockMessageWriter()

	msgs := []Message{
		{Topic: "t1", Key: []byte("k1"), Value: []byte("v1")},
		{Topic: "t2", Key: []byte("k2"), Value: []byte("v2")},
	}

	if err := mw.WriteMessages(context.Background(), msgs); err != nil {
		t.Fatalf("WriteMessages failed: %v", err)
	}

	got := mw.Messages()
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}

	_ = mw.Close()
	if !mw.closed {
		t.Fatal("expected closed to be true")
	}
}
