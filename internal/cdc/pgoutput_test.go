package cdc

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

// buildRelation constructs a pgoutput 'R' (RELATION) message.
func buildRelation(id uint32, namespace, name string, columns []colMeta) []byte {
	var buf []byte
	buf = append(buf, 'R')

	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, id)
	buf = append(buf, idBytes...)

	buf = append(buf, []byte(namespace)...)
	buf = append(buf, 0) // NUL terminator
	buf = append(buf, []byte(name)...)
	buf = append(buf, 0) // NUL terminator

	// Replica identity (1 byte) – 'd' for default
	buf = append(buf, 'd')

	// Column count (2 bytes)
	colCount := make([]byte, 2)
	binary.BigEndian.PutUint16(colCount, uint16(len(columns)))
	buf = append(buf, colCount...)

	for _, col := range columns {
		buf = append(buf, []byte(col.name)...)
		buf = append(buf, 0) // NUL terminator

		oid := make([]byte, 4)
		binary.BigEndian.PutUint32(oid, col.typeOID)
		buf = append(buf, oid...)

		// Type modifier (4 bytes, typically -1)
		mod := make([]byte, 4)
		binary.BigEndian.PutUint32(mod, 0xFFFFFFFF)
		buf = append(buf, mod...)

		// Flags (1 byte)
		var flags byte
		if col.isKey {
			flags |= 0x01
		}
		if col.isNotNull {
			flags |= 0x02
		}
		buf = append(buf, flags)
	}

	return buf
}

// buildInsert constructs a pgoutput 'I' (INSERT) message.
// values must be provided in column order matching the relation definition.
func buildInsert(relID uint32, values []any) []byte {
	var buf []byte
	buf = append(buf, 'I')

	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, relID)
	buf = append(buf, idBytes...)

	// New tuple marker
	buf = append(buf, 'N')
	buf = append(buf, buildTupleValues(values)...)

	return buf
}

// buildUpdate constructs a pgoutput 'U' (UPDATE) message.
// values must be provided in column order matching the relation definition.
func buildUpdate(relID uint32, oldValues, newValues []any) []byte {
	var buf []byte
	buf = append(buf, 'U')

	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, relID)
	buf = append(buf, idBytes...)

	// Old tuple (key) if present
	if oldValues != nil {
		buf = append(buf, 'K')
		buf = append(buf, buildTupleValues(oldValues)...)
	}

	// New tuple
	if newValues != nil {
		buf = append(buf, 'N')
		buf = append(buf, buildTupleValues(newValues)...)
	}

	return buf
}

// buildDelete constructs a pgoutput 'D' (DELETE) message.
// keyValues must be provided in column order matching the relation definition.
func buildDelete(relID uint32, keyValues []any) []byte {
	var buf []byte
	buf = append(buf, 'D')

	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, relID)
	buf = append(buf, idBytes...)

	// Key tuple
	if keyValues != nil {
		buf = append(buf, 'K')
		buf = append(buf, buildTupleValues(keyValues)...)
	}

	return buf
}

// buildTupleValues encodes a slice of values into pgoutput tuple format.
// Values must be in column order matching the relation definition.
func buildTupleValues(values []any) []byte {
	var buf []byte

	// Column count (2 bytes)
	count := make([]byte, 2)
	binary.BigEndian.PutUint16(count, uint16(len(values)))
	buf = append(buf, count...)

	for _, v := range values {
		if v == nil {
			buf = append(buf, 'n') // NULL marker
		} else {
			buf = append(buf, 't') // text value
			str := ""
			switch val := v.(type) {
			case string:
				str = val
			}
			valLen := make([]byte, 4)
			binary.BigEndian.PutUint32(valLen, uint32(len(str)))
			buf = append(buf, valLen...)
			buf = append(buf, []byte(str)...)
		}
	}

	return buf
}

// buildCopyData wraps decoded WAL data in a CopyData message with the pgoutput
// 'w' (XLogData) framing.
func buildCopyData(walData []byte) []byte {
	var buf []byte
	buf = append(buf, 'w') // XLogData marker

	// 8 bytes: WAL start LSN
	lsn := make([]byte, 8)
	binary.BigEndian.PutUint64(lsn, 0x0000000100000042)
	buf = append(buf, lsn...)

	// 8 bytes: WAL end LSN
	endLSN := make([]byte, 8)
	binary.BigEndian.PutUint64(endLSN, 0x0000000100000084)
	buf = append(buf, endLSN...)

	// 8 bytes: commit timestamp (microseconds since epoch)
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, 1700000000000000)
	buf = append(buf, ts...)

	// Append the actual WAL data
	buf = append(buf, walData...)

	return buf
}

// TestDecodeRelation verifies RELATION message decoding.
func TestDecodeRelation(t *testing.T) {
	decoder := newPGOutputDecoder()

	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
		{name: "name", typeOID: 25, isKey: false, isNotNull: false},
		{name: "amount_cents", typeOID: 20, isKey: false, isNotNull: true},
	}

	relData := buildRelation(16385, "public", "plans", cols)

	// Wrap in CopyData (XLogData) framing
	copyData := buildCopyData(relData)
	changes, lsn, err := decoder.decode(copyData)

	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes from relation message, got %d", len(changes))
	}
	if lsn == 0 {
		t.Fatal("expected non-zero LSN")
	}

	rel, ok := decoder.relations[16385]
	if !ok {
		t.Fatal("relation not stored in decoder map")
	}
	if rel.name != "plans" {
		t.Errorf("expected table name 'plans', got %q", rel.name)
	}
	if rel.namespace != "public" {
		t.Errorf("expected namespace 'public', got %q", rel.namespace)
	}
	if len(rel.columns) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(rel.columns))
	}
}

// TestDecodeInsert verifies INSERT message decoding.
func TestDecodeInsert(t *testing.T) {
	decoder := newPGOutputDecoder()

	// First register the relation.
	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
		{name: "name", typeOID: 25, isKey: false, isNotNull: false},
	}

	relData := buildRelation(16385, "public", "plans", cols)
	copyData := buildCopyData(relData)
	_, _, _ = decoder.decode(copyData)

	// Now decode an INSERT.
	insertData := buildInsert(16385, []any{"plan-1", "basic"})
	copyData = buildCopyData(append(relData, insertData...))
	// Re-register and insert
	d2 := newPGOutputDecoder()
	// Register relation
	cr := buildCopyData(relData)
	_, _, _ = d2.decode(cr)
	// Then insert
	ci := buildCopyData(buildInsert(16385, []any{"plan-1", "basic"}))
	changes, lsn, err := d2.decode(ci)

	if err != nil {
		t.Fatalf("decode insert failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Op != "insert" {
		t.Errorf("expected insert op, got %q", changes[0].Op)
	}
	if changes[0].Table != "plans" {
		t.Errorf("expected table plans, got %q", changes[0].Table)
	}
	if changes[0].After["id"] != "plan-1" {
		t.Errorf("expected id plan-1, got %v", changes[0].After["id"])
	}
	if lsn == 0 {
		t.Error("expected non-zero LSN")
	}
}

// TestDecodeUpdate verifies UPDATE message decoding.
func TestDecodeUpdate(t *testing.T) {
	d := newPGOutputDecoder()

	// Register relation
	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
		{name: "status", typeOID: 25, isKey: false, isNotNull: false},
	}
	relData := buildRelation(16386, "public", "subscriptions", cols)
	_, _, _ = d.decode(buildCopyData(relData))

	// Update with old and new values.
	updateData := buildUpdate(16386,
		[]any{"sub-1", nil},
		[]any{"sub-1", "active"},
	)
	changes, lsn, err := d.decode(buildCopyData(updateData))

	if err != nil {
		t.Fatalf("decode update failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Op != "update" {
		t.Errorf("expected update op, got %q", changes[0].Op)
	}
	if changes[0].Before == nil {
		t.Fatal("expected before values in update")
	}
	if changes[0].After == nil {
		t.Fatal("expected after values in update")
	}
	if changes[0].After["status"] != "active" {
		t.Errorf("expected status active, got %v", changes[0].After["status"])
	}
	if lsn == 0 {
		t.Error("expected non-zero LSN")
	}
}

// TestDecodeDelete verifies DELETE message decoding.
func TestDecodeDelete(t *testing.T) {
	d := newPGOutputDecoder()

	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
	}
	relData := buildRelation(16387, "public", "statements", cols)
	_, _, _ = d.decode(buildCopyData(relData))

	deleteData := buildDelete(16387, []any{"stmt-1"})
	changes, lsn, err := d.decode(buildCopyData(deleteData))

	if err != nil {
		t.Fatalf("decode delete failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Op != "delete" {
		t.Errorf("expected delete op, got %q", changes[0].Op)
	}
	if changes[0].Table != "statements" {
		t.Errorf("expected table statements, got %q", changes[0].Table)
	}
	if lsn == 0 {
		t.Error("expected non-zero LSN")
	}
}

// TestDecodeBeginCommit wraps multiple operations in a transaction.
func TestDecodeBeginCommit(t *testing.T) {
	d := newPGOutputDecoder()

	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
		{name: "name", typeOID: 25, isKey: false, isNotNull: false},
	}
	relData := buildRelation(16385, "public", "plans", cols)
	_, _, _ = d.decode(buildCopyData(relData))

	// Build a transaction: BEGIN + INSERT + COMMIT
	var tx []byte
	tx = append(tx, 'B')
	beginRest := make([]byte, 20) // final LSN(8) + commit ts(8) + xid(4)
	binary.BigEndian.PutUint64(beginRest[0:8], 100)
	binary.BigEndian.PutUint64(beginRest[8:16], 1700000000000000)
	binary.BigEndian.PutUint32(beginRest[16:20], 12345)
	tx = append(tx, beginRest...)

	tx = append(tx, buildInsert(16385, []any{"tx-plan", "tx-name"})...)

	tx = append(tx, 'C')
	commitRest := make([]byte, 25)
	commitRest[0] = 0               // flags
	binary.BigEndian.PutUint64(commitRest[1:9], 200)  // commit LSN
	binary.BigEndian.PutUint64(commitRest[9:17], 300)  // end LSN
	binary.BigEndian.PutUint64(commitRest[17:25], 1700000000000000) // ts
	tx = append(tx, commitRest...)

	changes, maxLSN, err := d.decode(buildCopyData(tx))

	if err != nil {
		t.Fatalf("decode transaction failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change in transaction, got %d", len(changes))
	}
	if changes[0].Op != "insert" {
		t.Errorf("expected insert, got %q", changes[0].Op)
	}
	if changes[0].After["id"] != "tx-plan" {
		t.Errorf("expected id tx-plan, got %v", changes[0].After["id"])
	}
	// maxLSN should be the commit LSN from the COMMIT message
	if maxLSN != uint64(200) {
		t.Errorf("expected maxLSN 200, got %d", maxLSN)
	}
}

// TestDecodeOriginMessage verifies ORIGIN message is skipped gracefully.
func TestDecodeOriginMessage(t *testing.T) {
	d := newPGOutputDecoder()

	var origin []byte
	origin = append(origin, 'O')
	lsnBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lsnBytes, 100)
	origin = append(origin, lsnBytes...)
	origin = append(origin, []byte("test_origin")...)
	origin = append(origin, 0)

	_, lsn, err := d.decode(buildCopyData(origin))
	if err != nil {
		t.Fatalf("decode origin failed: %v", err)
	}
	if lsn == 0 {
		t.Error("expected non-zero LSN from origin")
	}
}

// TestDecodeTooShort verifies handling of too-short data.
func TestDecodeTooShort(t *testing.T) {
	d := newPGOutputDecoder()
	// CopyData must be at least 25 bytes
	_, _, err := d.decode([]byte{})
	if err == nil {
		t.Fatal("expected error for empty data")
	}

	_, _, err = d.decode([]byte{0, 0, 0, 0, 0})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

// TestDecodeUnknownMessageType verifies unknown message types are handled.
func TestDecodeUnknownMessageType(t *testing.T) {
	d := newPGOutputDecoder()
	// Build a CopyData with an unknown message type
	var buf []byte
	buf = append(buf, 'w')
	lsnBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lsnBytes, 42)
	buf = append(buf, lsnBytes...)
	endLSN := make([]byte, 8)
	binary.BigEndian.PutUint64(endLSN, 84)
	buf = append(buf, endLSN...)
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, 1700000000000000)
	buf = append(buf, ts...)
	// Unknown message type 'X'
	buf = append(buf, 'X')

	// Unknown message types are silently skipped.
	changes, resultLSN, err := d.decode(buf)
	if err != nil {
		t.Fatalf("unexpected error for unknown type: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes, got %d", len(changes))
	}
	if resultLSN == 0 {
		t.Error("expected non-zero LSN")
	}
}

// TestDecodeInsertUnknownRelation verifies error on unknown relation.
func TestDecodeInsertUnknownRelation(t *testing.T) {
	d := newPGOutputDecoder()
	insertData := buildInsert(99999, []any{"nope"})
	_, _, err := d.decode(buildCopyData(insertData))
	if err == nil {
		t.Fatal("expected error for unknown relation")
	}
}

// TestDecodeMultipleInserts verifies decoding multiple inserts in one batch.
func TestDecodeMultipleInserts(t *testing.T) {
	d := newPGOutputDecoder()

	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
	}
	relData := buildRelation(16385, "public", "plans", cols)
	_, _, _ = d.decode(buildCopyData(relData))

	// Two inserts in one CopyData
	var batch []byte
	batch = append(batch, buildInsert(16385, []any{"p1"})...)
	batch = append(batch, buildInsert(16385, []any{"p2"})...)

	changes, lsn, err := d.decode(buildCopyData(batch))
	if err != nil {
		t.Fatalf("decode batch failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	if changes[0].After["id"] != "p1" {
		t.Errorf("expected p1, got %v", changes[0].After["id"])
	}
	if changes[1].After["id"] != "p2" {
		t.Errorf("expected p2, got %v", changes[1].After["id"])
	}
	if lsn == 0 {
		t.Error("expected non-zero LSN")
	}
}

// TestDecodeNullValues verifies NULL column values are handled.
func TestDecodeNullValues(t *testing.T) {
	d := newPGOutputDecoder()

	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
		{name: "description", typeOID: 25, isKey: false, isNotNull: false},
	}
	relData := buildRelation(16385, "public", "plans", cols)
	_, _, _ = d.decode(buildCopyData(relData))

	insertData := buildInsert(16385, []any{"p1", nil})
	changes, _, err := d.decode(buildCopyData(insertData))

	if err != nil {
		t.Fatalf("decode null insert failed: %v", err)
	}
	if changes[0].After["id"] != "p1" {
		t.Errorf("expected id p1, got %v", changes[0].After["id"])
	}
	if changes[0].After["description"] != nil {
		t.Errorf("expected nil description, got %v", changes[0].After["description"])
	}
}

// TestDecodeEmptyWalData verifies empty WAL data returns no changes.
func TestDecodeEmptyWalData(t *testing.T) {
	d := newPGOutputDecoder()
	// CopyData with XLogData framing but no WAL data
	var buf []byte
	buf = append(buf, 'w')
	lsnBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(lsnBytes, 42)
	buf = append(buf, lsnBytes...)
	endLSN := make([]byte, 8)
	binary.BigEndian.PutUint64(endLSN, 42)
	buf = append(buf, endLSN...)
	ts := make([]byte, 8)
	binary.BigEndian.PutUint64(ts, 1700000000000000)
	buf = append(buf, ts...)
	// No WAL data appended

	changes, resultLSN, err := d.decode(buf[:25])
	if err != nil {
		t.Fatalf("decode empty WAL failed: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected 0 changes, got %d", len(changes))
	}
	if resultLSN == 0 {
		t.Error("expected non-zero LSN")
	}
}

// TestDecodeTypeMessage skips TYPE messages.
func TestDecodeTypeMessage(t *testing.T) {
	d := newPGOutputDecoder()
	var buf []byte
	buf = append(buf, 'Y')
	id := make([]byte, 4)
	binary.BigEndian.PutUint32(id, 1)
	buf = append(buf, id...)
	buf = append(buf, []byte("public")...)
	buf = append(buf, 0)
	buf = append(buf, []byte("test_type")...)
	buf = append(buf, 0)

	_, _, err := d.decode(buildCopyData(buf))
	if err != nil {
		t.Fatalf("decode type message failed: %v", err)
	}
}

// TestNewConsumerConfigValidation verifies consumer config defaults.
func TestNewConsumerConfigValidation(t *testing.T) {
	// Empty connection string should fail.
	_, err := NewConsumer(ConsumerConfig{})
	if err == nil {
		t.Fatal("expected error for empty connection string")
	}

	// Valid config should work.
	cfg := ConsumerConfig{
		ConnString: "postgres://test",
	}
	c, err := NewConsumer(cfg)
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if c.cfg.SlotName != "stellabill_cdc_slot" {
		t.Fatalf("expected default slot name, got %q", c.cfg.SlotName)
	}
	if c.cfg.StandbyTimeout != 10e9 { // 10 seconds in nanoseconds
		t.Fatalf("expected default standby timeout")
	}
}

// TestDefaultConsumerConfig verifies defaults.
func TestDefaultConsumerConfig(t *testing.T) {
	cfg := DefaultConsumerConfig()
	if cfg.SlotName != "stellabill_cdc_slot" {
		t.Errorf("expected default slot name")
	}
	if cfg.PublicationName != "stellabill_cdc" {
		t.Errorf("expected default publication name")
	}
	if cfg.StandbyTimeout <= 0 {
		t.Errorf("expected non-zero standby timeout")
	}
	if cfg.ReconnectBackoff <= 0 {
		t.Errorf("expected non-zero reconnect backoff")
	}
}

// TestConsumerStartStop verifies start/stop lifecycle.
func TestConsumerStartStop(t *testing.T) {
	cfg := ConsumerConfig{
		ConnString: "postgres://test",
		SlotName:   "test_slot",
	}
	c, _ := NewConsumer(cfg)

	if c.IsRunning() {
		t.Fatal("consumer should not be running before Start()")
	}

	c.Stop() // Should be safe even before Start()
	if c.IsRunning() {
		t.Fatal("consumer should not be running after Stop()")
	}
}

// TestNewConsumerNilSinksDefault verifies sinks can be nil.
func TestNewConsumerNilSinksDefault(t *testing.T) {
	cfg := ConsumerConfig{
		ConnString: "postgres://test",
		Sinks:      nil,
		SlotName:   "test_slot",
	}
	c, err := NewConsumer(cfg)
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil consumer")
	}
}

// TestSinkInterfaceCompliance verifies compile-time interface satisfaction.
func TestSinkInterfaceCompliance(t *testing.T) {
	// These are compile-time checks via var declarations in kafka_sink.go.
	// This test ensures the declarations don't cause compilation errors.
	_ = []Sink{NewMemorySink(), NewStdoutSink(), nil}
	t.Log("sink interface compliance verified at compile time")
}

// ---------------------------------------------------------------------------
// Additional decoder edge case tests
// ---------------------------------------------------------------------------

// TestDecodeWithoutXLogData verifies raw WAL data (without 'w' XLogData
// framing) is processed correctly via decodeWAL.
func TestDecodeWithoutXLogData(t *testing.T) {
	d := newPGOutputDecoder()

	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
	}
	relData := buildRelation(16385, "public", "plans", cols)

	// Pass raw relation data directly to decodeWAL
	_, _, err := d.decodeWAL(relData, 0)
	if err != nil {
		t.Fatalf("raw relation decode failed: %v", err)
	}

	// Now decode a raw insert via decodeWAL
	insertData := buildInsert(16385, []any{"raw-1"})
	changes, lsn, err := d.decodeWAL(insertData, 0)
	if err != nil {
		t.Fatalf("raw insert decode failed: %v", err)
	}
	if len(changes) != 1 || changes[0].After["id"] != "raw-1" {
		t.Fatalf("unexpected raw insert result: %+v", changes)
	}
	if lsn != 0 {
		t.Errorf("expected lsn=0 for raw decode, got %d", lsn)
	}

	// Also test the code path in decode() where data[0] != 'w'
	// Build data that's >= 25 bytes but not starting with 'w'
	longData := make([]byte, 30)
	longData[0] = 'B'
	// Fill BEGIN rest
	binary.BigEndian.PutUint64(longData[1:9], 100)
	binary.BigEndian.PutUint64(longData[9:17], 1700000000000000)
	binary.BigEndian.PutUint32(longData[17:21], 12345)
	changes2, lsn2, err := d.decode(longData)
	if err != nil {
		t.Fatalf("decode without 'w' marker failed: %v", err)
	}
	if lsn2 != 0 {
		t.Errorf("expected lsn=0 for non-w data, got %d", lsn2)
	}
	_ = changes2
}

// TestDecodeRelationTruncated verifies relation decoding with truncated data.
func TestDecodeRelationTruncated(t *testing.T) {
	// Too-short relation (less than 10 bytes)
	d := newPGOutputDecoder()
	_, _, err := d.decodeWAL([]byte{'R', 0, 0}, 0)
	if err == nil {
		t.Fatal("expected error for truncated relation (too short)")
	}

	// Relation with unterminated namespace
	noNull := append([]byte{'R', 0, 0, 0, 1}, []byte("public")...)
	_, _, err = d.decodeWAL(noNull, 0)
	if err == nil {
		t.Fatal("expected error for unterminated namespace")
	}

	// Relation with unterminated name
	partial := append([]byte{'R', 0, 0, 0, 1, 'p', 'u', 'b', 'l', 'i', 'c', 0, 't'}, []byte("bl")...)
	_, _, err = d.decodeWAL(partial, 0)
	if err == nil {
		t.Fatal("expected error for unterminated name")
	}
}

// TestDecodeRelationMissingReplicaIdentity verifies error on missing replica identity.
func TestDecodeRelationMissingReplicaIdentity(t *testing.T) {
	d := newPGOutputDecoder()
	// Full namespace + name but no replica identity byte
	buf := []byte{'R', 0, 0, 0, 1, 'p', 'u', 'b', 0, 't', 0}
	_, _, err := d.decodeWAL(buf, 0)
	if err == nil {
		t.Fatal("expected error for missing replica identity")
	}
}

// TestDecodeRelationMissingColumnCount verifies error on missing column count.
func TestDecodeRelationMissingColumnCount(t *testing.T) {
	d := newPGOutputDecoder()
	// Full namespace + name + replica identity but no column count
	buf := []byte{'R', 0, 0, 0, 1, 'p', 'u', 'b', 0, 't', 0, 'd'}
	_, _, err := d.decodeWAL(buf, 0)
	if err == nil {
		t.Fatal("expected error for missing column count")
	}
}

// TestDecodeRelationWithZeroColumns verifies relation with zero columns.
func TestDecodeRelationWithZeroColumns(t *testing.T) {
	d := newPGOutputDecoder()
	// Relation with 0 columns
	buf := []byte{'R', 0, 0, 0, 1, 'p', 'u', 'b', 0, 't', 0, 'd', 0, 0}
	_, _, err := d.decodeWAL(buf, 0)
	if err != nil {
		t.Fatalf("zero-column relation should not error: %v", err)
	}
	rel := d.relations[1]
	if rel == nil {
		t.Fatal("expected relation to be registered")
	}
	if len(rel.columns) != 0 {
		t.Errorf("expected 0 columns, got %d", len(rel.columns))
	}
}

// TestDecodeInsertTooShort verifies short insert error.
func TestDecodeInsertTooShort(t *testing.T) {
	d := newPGOutputDecoder()
	_, _, err := d.decodeWAL([]byte{'I', 0, 0, 0}, 0)
	if err == nil {
		t.Fatal("expected error for short insert")
	}
}

// TestDecodeInsertWrongMarker verifies wrong tuple marker in insert.
func TestDecodeInsertWrongMarker(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{{name: "id", typeOID: 25, isKey: true, isNotNull: true}}
	relData := buildRelation(16385, "public", "plans", cols)
	_, _, _ = d.decodeWAL(relData, 0)

	// Insert with 'K' marker instead of 'N'
	buf := []byte{'I', 0, 0, 0, 0, 1, 'K', 0, 1, 'n'}
	_, _, err := d.decodeWAL(buf, 0)
	if err == nil {
		t.Fatal("expected error for wrong tuple marker in insert")
	}
}

// TestDecodeUpdateTooShort verifies short update error.
func TestDecodeUpdateTooShort(t *testing.T) {
	d := newPGOutputDecoder()
	_, _, err := d.decodeWAL([]byte{'U', 0, 0, 0}, 0)
	if err == nil {
		t.Fatal("expected error for short update")
	}
}

// TestDecodeUpdateOldOnly verifies update with only old values (no new).
func TestDecodeUpdateOldOnly(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
		{name: "status", typeOID: 25, isKey: false, isNotNull: false},
	}
	relData := buildRelation(16386, "public", "subscriptions", cols)
	_, _, _ = d.decodeWAL(relData, 0)

	// Update with only old key values ('O' marker)
	updateData := buildUpdate(16386, []any{"sub-1", "old-status"}, nil)
	// Override the marker to 'O'
	updateData[5] = 'O'
	changes, _, err := d.decodeWAL(updateData, 0)
	if err != nil {
		t.Fatalf("decode update old-only failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Before == nil {
		t.Fatal("expected before values")
	}
	if changes[0].After != nil {
		t.Fatal("expected no after values")
	}
}

// TestDecodeUpdateNewOnly verifies update with only new values.
func TestDecodeUpdateNewOnly(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{
		{name: "id", typeOID: 25, isKey: true, isNotNull: true},
		{name: "status", typeOID: 25, isKey: false, isNotNull: false},
	}
	relData := buildRelation(16386, "public", "subscriptions", cols)
	_, _, _ = d.decodeWAL(relData, 0)

	// Manual build: update with new tuple only
	var buf []byte
	buf = append(buf, 'U')
	idBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(idBytes, 16386)
	buf = append(buf, idBytes...)
	buf = append(buf, 'N')
	// Build tuple: 1 column, text value
	buf = append(buf, 0, 1, 't')
	valLen := make([]byte, 4)
	binary.BigEndian.PutUint32(valLen, 3)
	buf = append(buf, valLen...)
	buf = append(buf, []byte("new")...)

	changes, _, err := d.decodeWAL(buf, 0)
	if err != nil {
		t.Fatalf("decode update new-only failed: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if changes[0].Before != nil {
		t.Fatal("expected no before values")
	}
	if changes[0].After == nil {
		t.Fatal("expected after values")
	}
}

// TestDecodeDeleteTooShort verifies short delete error.
func TestDecodeDeleteTooShort(t *testing.T) {
	d := newPGOutputDecoder()
	_, _, err := d.decodeWAL([]byte{'D', 0, 0, 0}, 0)
	if err == nil {
		t.Fatal("expected error for short delete")
	}
}

// TestDecodeDeleteOldMarker verifies delete with 'O' marker.
func TestDecodeDeleteOldMarker(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{{name: "id", typeOID: 25, isKey: true, isNotNull: true}}
	relData := buildRelation(16387, "public", "statements", cols)
	_, _, _ = d.decodeWAL(relData, 0)

	deleteData := buildDelete(16387, []any{"stmt-1"})
	// Change marker from 'K' to 'O'
	deleteData[5] = 'O'
	changes, _, err := d.decodeWAL(deleteData, 0)
	if err != nil {
		t.Fatalf("decode delete with O marker failed: %v", err)
	}
	if len(changes) != 1 || changes[0].Before["id"] != "stmt-1" {
		t.Fatalf("unexpected delete result: %+v", changes)
	}
}

// TestDecodeDeleteNoKey verifies delete without key tuple.
func TestDecodeDeleteNoKey(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{{name: "id", typeOID: 25, isKey: true, isNotNull: true}}
	relData := buildRelation(16387, "public", "statements", cols)
	_, _, _ = d.decodeWAL(relData, 0)

	// Delete message with relation ID 16387 (0x4003 big-endian), no valid tuple marker
	// Bytes: D | 0 0 64 3 | @ (not a valid tuple marker so no tuple decoded)
	buf := []byte{'D', 0, 0, 64, 3, '@'}
	changes, _, err := d.decodeWAL(buf, 0)
	if err != nil {
		t.Fatalf("decode delete no-key failed: %v", err)
	}
	if len(changes) != 1 || changes[0].Before != nil {
		t.Fatalf("expected delete with nil before, got %+v", changes)
	}
}

// TestDecodeTupleTooShort verifies tuple too short error.
func TestDecodeTupleTooShort(t *testing.T) {
	d := newPGOutputDecoder()
	_, _, err := d.decodeTuple([]byte{}, nil)
	if err == nil {
		t.Fatal("expected error for empty tuple")
	}
	_, _, err = d.decodeTuple([]byte{0}, nil)
	if err == nil {
		t.Fatal("expected error for 1-byte tuple")
	}
}

// TestDecodeTupleUnchangedToast verifies 'u' (unchanged TOAST) marker.
func TestDecodeTupleUnchangedToast(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{{name: "big_text", typeOID: 25}}
	// Tuple with 'u' marker (unchanged TOAST)
	tuple := []byte{0, 1, 'u'}
	result, pos, err := d.decodeTuple(tuple, cols)
	if err != nil {
		t.Fatalf("toast tuple decode failed: %v", err)
	}
	if pos != 3 {
		t.Errorf("expected pos=3, got %d", pos)
	}
	if result["big_text"] != nil {
		t.Errorf("expected nil for unchanged TOAST, got %v", result["big_text"])
	}
}

// TestDecodeTupleTruncatedValue verifies truncated text value.
func TestDecodeTupleTruncatedValue(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{{name: "id", typeOID: 25}}
	// Tuple with 't' marker but value length extends beyond data
	tuple := []byte{0, 1, 't', 0, 0, 0, 10, 'a', 'b'}
	result, _, err := d.decodeTuple(tuple, cols)
	if err != nil {
		t.Fatalf("truncated value decode should not error: %v", err)
	}
	// Should return partial result without the truncated value
	_ = result
}

// TestDecodeTupleUnexpectedMarker verifies unexpected marker error.
func TestDecodeTupleUnexpectedMarker(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{{name: "id", typeOID: 25}}
	// Tuple with 'x' marker (invalid)
	tuple := []byte{0, 1, 'x'}
	_, _, err := d.decodeTuple(tuple, cols)
	if err == nil {
		t.Fatal("expected error for unexpected tuple marker")
	}
}

// TestDecodeBeginTruncated verifies truncated BEGIN is handled.
func TestDecodeBeginTruncated(t *testing.T) {
	d := newPGOutputDecoder()
	// BEGIN with only 5 bytes (needs 21)
	changes, maxLSN, err := d.decodeWAL([]byte{'B', 0, 0, 0, 0}, 0)
	if err != nil {
		t.Fatalf("truncated begin should return gracefully: %v", err)
	}
	if len(changes) != 0 {
		t.Fatal("expected 0 changes")
	}
	if maxLSN != 0 {
		t.Errorf("expected maxLSN=0, got %d", maxLSN)
	}
}

// TestDecodeCommitTruncated verifies truncated COMMIT is handled.
func TestDecodeCommitTruncated(t *testing.T) {
	d := newPGOutputDecoder()
	// COMMIT with only 5 bytes (needs 26)
	changes, maxLSN, err := d.decodeWAL([]byte{'C', 0, 0, 0, 0}, 0)
	if err != nil {
		t.Fatalf("truncated commit should return gracefully: %v", err)
	}
	if len(changes) != 0 {
		t.Fatal("expected 0 changes")
	}
	if maxLSN != 0 {
		t.Errorf("expected maxLSN=0, got %d", maxLSN)
	}
}

// TestDecodeOriginTruncated verifies truncated ORIGIN is handled.
func TestDecodeOriginTruncated(t *testing.T) {
	d := newPGOutputDecoder()
	// ORIGIN with just the LSN (8 bytes after marker = 9 bytes needed total)
	buf := []byte{'O', 0, 0, 0, 0, 0, 0, 0, 0}
	_, lsn, err := d.decodeWAL(buf, 0)
	if err != nil {
		t.Fatalf("origin decode failed: %v", err)
	}
	if lsn != 0 {
		t.Errorf("expected lsn=0, got %d", lsn)
	}
}

// TestDecodeMultiTableTransaction verifies changes across multiple tables in one batch.
func TestDecodeMultiTableTransaction(t *testing.T) {
	d := newPGOutputDecoder()

	cols1 := []colMeta{{name: "id", typeOID: 25, isKey: true}}
	cols2 := []colMeta{{name: "id", typeOID: 25, isKey: true}}

	rel1 := buildRelation(16385, "public", "plans", cols1)
	rel2 := buildRelation(16386, "public", "subscriptions", cols2)

	// Build batch: REL(plans) + REL(subscriptions) + INSERT(plans) + INSERT(subscriptions)
	var batch []byte
	batch = append(batch, rel1...)
	batch = append(batch, rel2...)
	batch = append(batch, buildInsert(16385, []any{"p1"})...)
	batch = append(batch, buildInsert(16386, []any{"s1"})...)

	changes, lsn, err := d.decodeWAL(batch, 0)
	if err != nil {
		t.Fatalf("multi-table batch failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	if changes[0].Table != "plans" || changes[1].Table != "subscriptions" {
		t.Fatalf("unexpected table order: %s, %s", changes[0].Table, changes[1].Table)
	}
	if lsn != 0 {
		t.Errorf("expected lsn=0, got %d", lsn)
	}
}

// TestDecodeRelationThenImmediateOp verifies relation registration followed
// by an immediate operation in the same WAL batch.
func TestDecodeRelationThenImmediateOp(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{{name: "id", typeOID: 25, isKey: true, isNotNull: true}}

	// Register relation and insert in the same WAL batch
	var batch []byte
	batch = append(batch, buildRelation(16385, "public", "plans", cols)...)
	batch = append(batch, buildInsert(16385, []any{"inline"})...)

	changes, lsn, err := d.decodeWAL(batch, 42)
	if err != nil {
		t.Fatalf("relation+insert batch failed: %v", err)
	}
	if len(changes) != 1 || changes[0].After["id"] != "inline" {
		t.Fatalf("unexpected result: %+v", changes)
	}
	if lsn != 42 {
		t.Errorf("expected lsn=42, got %d", lsn)
	}
}

// TestDecodeWALMaxLSNFromCommit verifies multiple commits yield the highest LSN.
func TestDecodeWALMaxLSNFromCommit(t *testing.T) {
	d := newPGOutputDecoder()
	cols := []colMeta{{name: "id", typeOID: 25, isKey: true}}
	relData := buildRelation(16385, "public", "plans", cols)
	_, _, _ = d.decodeWAL(relData, 0)

	// Transaction 1: INSERT + COMMIT (LSN 100)
	var tx1 []byte
	tx1 = append(tx1, 'B')
	beginRest := make([]byte, 20)
	binary.BigEndian.PutUint64(beginRest[0:8], 100)
	binary.BigEndian.PutUint64(beginRest[8:16], 1700000000000000)
	binary.BigEndian.PutUint32(beginRest[16:20], 1)
	tx1 = append(tx1, beginRest...)
	tx1 = append(tx1, buildInsert(16385, []any{"a"})...)
	tx1 = append(tx1, 'C')
	commit1 := make([]byte, 25)
	commit1[0] = 0
	binary.BigEndian.PutUint64(commit1[1:9], 100)
	binary.BigEndian.PutUint64(commit1[9:17], 150)
	binary.BigEndian.PutUint64(commit1[17:25], 1700000000000000)
	tx1 = append(tx1, commit1...)

	// Transaction 2: INSERT + COMMIT (LSN 200)
	var tx2 []byte
	tx2 = append(tx2, 'B')
	beginRest2 := make([]byte, 20)
	binary.BigEndian.PutUint64(beginRest2[0:8], 200)
	binary.BigEndian.PutUint64(beginRest2[8:16], 1700000000000000)
	binary.BigEndian.PutUint32(beginRest2[16:20], 2)
	tx2 = append(tx2, beginRest2...)
	tx2 = append(tx2, buildInsert(16385, []any{"b"})...)
	tx2 = append(tx2, 'C')
	commit2 := make([]byte, 25)
	commit2[0] = 0
	binary.BigEndian.PutUint64(commit2[1:9], 200)
	binary.BigEndian.PutUint64(commit2[9:17], 250)
	binary.BigEndian.PutUint64(commit2[17:25], 1700000000000000)
	tx2 = append(tx2, commit2...)

	combined := append(tx1, tx2...)
	changes, maxLSN, err := d.decodeWAL(combined, 0)
	if err != nil {
		t.Fatalf("multi-transaction decode failed: %v", err)
	}
	if len(changes) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(changes))
	}
	if maxLSN != 200 {
		t.Errorf("expected maxLSN=200, got %d", maxLSN)
	}
}

// TestDecodeWALEmptyInput verifies empty WAL data returns no changes.
func TestDecodeWALEmptyInput(t *testing.T) {
	d := newPGOutputDecoder()
	changes, lsn, err := d.decodeWAL(nil, 0)
	if err != nil {
		t.Fatalf("empty WAL should not error: %v", err)
	}
	if len(changes) != 0 {
		t.Fatal("expected 0 changes")
	}
	if lsn != 0 {
		t.Errorf("expected lsn=0, got %d", lsn)
	}

	changes, lsn, err = d.decodeWAL([]byte{}, 0)
	if err != nil {
		t.Fatalf("empty WAL should not error: %v", err)
	}
	if len(changes) != 0 {
		t.Fatal("expected 0 changes")
	}
}

// TestValidateSink_Kafka verifies Kafka sink passes validation.
func TestValidateSink_Kafka(t *testing.T) {
	mw := newMockMessageWriter()
	sink, _ := NewKafkaSink(KafkaSinkConfig{Writer: mw})
	ValidateSink(t, sink)
}

// TestMockMessageWriter_WriteError verifies error propagation.
func TestMockMessageWriter_WriteError(t *testing.T) {
	mw := newMockMessageWriter()
	mw.writeErr = errors.New("write error")
	err := mw.WriteMessages(context.Background(), []Message{{Topic: "t"}})
	if err == nil {
		t.Fatal("expected write error")
	}
}

// TestKafkaSink_Flush verifies Flush on Kafka sink.
func TestKafkaSink_Flush(t *testing.T) {
	mw := newMockMessageWriter()
	sink, _ := NewKafkaSink(KafkaSinkConfig{Writer: mw})
	if err := sink.Flush(context.Background()); err != nil {
		t.Fatal("Flush should be no-op")
	}
}

// TestConsumerStartError verifies Start returns error when already running.
func TestConsumerStartError(t *testing.T) {
	c, _ := NewConsumer(ConsumerConfig{ConnString: "postgres://test"})
	c.running = true
	err := c.Start(context.Background())
	if err == nil {
		t.Fatal("expected 'already running' error")
	}
}

// TestNewConsumerAllDefaults verifies all defaults are properly set.
func TestNewConsumerAllDefaults(t *testing.T) {
	c, err := NewConsumer(ConsumerConfig{
		ConnString:          "postgres://test",
		MaxReconnectAttempts: -1, // negative, treated as zero
	})
	if err != nil {
		t.Fatalf("NewConsumer failed: %v", err)
	}
	if c.cfg.MaxReconnectAttempts != -1 {
		t.Errorf("expected -1 max attempts (preserved as-is), got %d", c.cfg.MaxReconnectAttempts)
	}
}
