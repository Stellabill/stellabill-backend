package cdc

import (
	"encoding/binary"
	"fmt"
)

// pgoutput message type constants
const (
	pgOutputBegin    = 'B'
	pgOutputCommit   = 'C'
	pgOutputInsert   = 'I'
	pgOutputUpdate   = 'U'
	pgOutputDelete   = 'D'
	pgOutputRelation = 'R'
	pgOutputOrigin   = 'O'
	pgOutputType     = 'Y'
)

// relMeta holds metadata for a relation (table) received from the stream.
type relMeta struct {
	id        uint32
	namespace string
	name      string
	columns   []colMeta
}

type colMeta struct {
	name      string
	typeOID   uint32
	isKey     bool
	isNotNull bool
}

// pgOutputDecoder decodes the pgoutput binary protocol into Change events.
type pgOutputDecoder struct {
	// relations maps relation ID to table metadata.
	relations map[uint32]*relMeta
}

func newPGOutputDecoder() *pgOutputDecoder {
	return &pgOutputDecoder{
		relations: make(map[uint32]*relMeta),
	}
}

// decode parses a batch of pgoutput CopyData into Change events.
// It returns the decoded changes and the highest LSN seen.
func (d *pgOutputDecoder) decode(data []byte) ([]Change, uint64, error) {
	// First byte of pgoutput CopyData: 'w' (XLogData)
	// Followed by: 8 bytes start LSN, 8 bytes end LSN, 8 bytes timestamp
	// The remainder is the WAL record.
	if len(data) < 25 {
		return nil, 0, fmt.Errorf("cdc: CopyData too short: %d bytes", len(data))
	}

	// Verify XLogData marker
	if data[0] != 'w' {
		// Try to process raw WAL data without XLogData framing
		return d.decodeWAL(data, 0)
	}

	// Read the WAL start position (bytes 1-8)
	lsn := binary.BigEndian.Uint64(data[1:9])

	// The actual WAL data starts at byte 25.
	walData := data[25:]
	return d.decodeWAL(walData, lsn)
}

// decodeWAL decodes raw WAL record bytes.
func (d *pgOutputDecoder) decodeWAL(walData []byte, baseLSN uint64) ([]Change, uint64, error) {
	var changes []Change
	var maxLSN uint64

	if len(walData) == 0 {
		return changes, baseLSN, nil
	}

	offset := 0
	for offset < len(walData) {
		remaining := walData[offset:]
		if len(remaining) < 1 {
			break
		}

		msgType := remaining[0]
		offset++

		switch msgType {
		case pgOutputBegin:
			// BEGIN: skip 20 bytes (8 final LSN + 8 timestamp + 4 XID)
			if len(remaining) < 21 {
				return changes, maxLSN, nil
			}
			offset += 20

		case pgOutputCommit:
			// COMMIT: skip 1 (flags), read 8 (commit LSN), skip 16 (8+8)
			if len(remaining) < 26 {
				return changes, maxLSN, nil
			}
			commitLSN := binary.BigEndian.Uint64(remaining[2:10])
			if commitLSN > maxLSN {
				maxLSN = commitLSN
			}
			offset += 25

		case pgOutputRelation:
			rel, n, err := d.decodeRelation(remaining)
			if err != nil {
				return changes, maxLSN, fmt.Errorf("cdc: relation decode error: %w", err)
			}
			d.relations[rel.id] = rel
			offset += n - 1

		case pgOutputInsert:
			change, n, err := d.decodeInsert(remaining)
			if err != nil {
				return changes, maxLSN, fmt.Errorf("cdc: insert decode error: %w", err)
			}
			changes = append(changes, change)
			offset += n - 1

		case pgOutputUpdate:
			change, n, err := d.decodeUpdate(remaining)
			if err != nil {
				return changes, maxLSN, fmt.Errorf("cdc: update decode error: %w", err)
			}
			changes = append(changes, change)
			offset += n - 1

		case pgOutputDelete:
			change, n, err := d.decodeDelete(remaining)
			if err != nil {
				return changes, maxLSN, fmt.Errorf("cdc: delete decode error: %w", err)
			}
			changes = append(changes, change)
			offset += n - 1

		case pgOutputOrigin:
			// Skip until NUL terminator
			pos := 9
			for pos < len(remaining) && remaining[pos] != 0 {
				pos++
			}
			offset += pos + 1

		case pgOutputType:
			// Skip NUL-terminated schema and type name
			pos := 5
			for i := 0; i < 2 && pos < len(remaining); i++ {
				for pos < len(remaining) && remaining[pos] != 0 {
					pos++
				}
				pos++ // skip NUL
			}
			offset += pos

		default:
			// Unknown message type – stop processing this batch.
			offset = len(walData)
		}
	}

	if maxLSN == 0 {
		maxLSN = baseLSN
	}

	return changes, maxLSN, nil
}

// decodeRelation decodes a RELATION ('R') message.
func (d *pgOutputDecoder) decodeRelation(data []byte) (*relMeta, int, error) {
	if len(data) < 10 {
		return nil, 0, fmt.Errorf("cdc: relation too short: %d bytes", len(data))
	}

	relID := binary.BigEndian.Uint32(data[1:5])

	// Decode namespace
	nsStart := 5
	nsEnd := nsStart
	for nsEnd < len(data) && data[nsEnd] != 0 {
		nsEnd++
	}
	if nsEnd >= len(data) {
		return nil, 0, fmt.Errorf("cdc: unterminated namespace in relation")
	}
	namespace := string(data[nsStart:nsEnd])

	// Decode relation name
	nameStart := nsEnd + 1
	nameEnd := nameStart
	for nameEnd < len(data) && data[nameEnd] != 0 {
		nameEnd++
	}
	if nameEnd >= len(data) {
		return nil, 0, fmt.Errorf("cdc: unterminated name in relation")
	}
	name := string(data[nameStart:nameEnd])

	pos := nameEnd + 1
	if pos >= len(data) {
		return nil, 0, fmt.Errorf("cdc: missing replica identity in relation")
	}

	// Skip replica identity byte
	pos++

	if pos+2 > len(data) {
		return nil, 0, fmt.Errorf("cdc: missing column count in relation")
	}

	colCount := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2

	cols := make([]colMeta, 0, colCount)
	for i := 0; i < colCount; i++ {
		if pos >= len(data) {
			break
		}

		// Column name (NUL-terminated)
		colNameStart := pos
		for pos < len(data) && data[pos] != 0 {
			pos++
		}
		if pos >= len(data) {
			break
		}
		colName := string(data[colNameStart:pos])
		pos++

		// Type OID (4 bytes) + type modifier (4 bytes)
		if pos+8 > len(data) {
			break
		}
		typeOID := binary.BigEndian.Uint32(data[pos : pos+4])
		pos += 8

		// Flags (1 byte)
		if pos >= len(data) {
			break
		}
		flags := data[pos]
		pos++

		cols = append(cols, colMeta{
			name:      colName,
			typeOID:   typeOID,
			isKey:     (flags & 0x01) != 0,
			isNotNull: (flags & 0x02) != 0,
		})
	}

	return &relMeta{
		id:        relID,
		namespace: namespace,
		name:      name,
		columns:   cols,
	}, pos, nil
}

// decodeInsert decodes an INSERT ('I') message.
func (d *pgOutputDecoder) decodeInsert(data []byte) (Change, int, error) {
	if len(data) < 6 {
		return Change{}, 0, fmt.Errorf("cdc: insert too short: %d bytes", len(data))
	}

	relID := binary.BigEndian.Uint32(data[1:5])

	if data[5] != 'N' {
		return Change{}, 0, fmt.Errorf("cdc: expected new tuple marker in insert, got %c", data[5])
	}

	rel, ok := d.relations[relID]
	if !ok {
		return Change{}, 0, fmt.Errorf("cdc: unknown relation ID %d", relID)
	}

	after, n, err := d.decodeTuple(data[6:], rel.columns)
	if err != nil {
		return Change{}, 0, err
	}

	return Change{
		Schema: rel.namespace,
		Table:  rel.name,
		Op:     "insert",
		After:  after,
	}, 6 + n, nil
}

// decodeUpdate decodes an UPDATE ('U') message.
func (d *pgOutputDecoder) decodeUpdate(data []byte) (Change, int, error) {
	if len(data) < 6 {
		return Change{}, 0, fmt.Errorf("cdc: update too short: %d bytes", len(data))
	}

	relID := binary.BigEndian.Uint32(data[1:5])

	rel, ok := d.relations[relID]
	if !ok {
		return Change{}, 0, fmt.Errorf("cdc: unknown relation ID %d", relID)
	}

	pos := 5
	var before map[string]any
	var after map[string]any

	if pos < len(data) {
		switch data[pos] {
		case 'K', 'O':
			var n int
			var err error
			before, n, err = d.decodeTuple(data[pos+1:], rel.columns)
			if err != nil {
				return Change{}, 0, err
			}
			pos += 1 + n
		}
	}

	if pos < len(data) && data[pos] == 'N' {
		var n int
		var err error
		after, n, err = d.decodeTuple(data[pos+1:], rel.columns)
		if err != nil {
			return Change{}, 0, err
		}
		pos += 1 + n
	}

	return Change{
		Schema: rel.namespace,
		Table:  rel.name,
		Op:     "update",
		Before: before,
		After:  after,
	}, pos, nil
}

// decodeDelete decodes a DELETE ('D') message.
func (d *pgOutputDecoder) decodeDelete(data []byte) (Change, int, error) {
	if len(data) < 6 {
		return Change{}, 0, fmt.Errorf("cdc: delete too short: %d bytes", len(data))
	}

	relID := binary.BigEndian.Uint32(data[1:5])

	rel, ok := d.relations[relID]
	if !ok {
		return Change{}, 0, fmt.Errorf("cdc: unknown relation ID %d", relID)
	}

	pos := 5
	var before map[string]any

	if pos < len(data) {
		switch data[pos] {
		case 'K', 'O':
			var n int
			var err error
			before, n, err = d.decodeTuple(data[pos+1:], rel.columns)
			if err != nil {
				return Change{}, 0, err
			}
			pos += 1 + n
		}
	}

	return Change{
		Schema: rel.namespace,
		Table:  rel.name,
		Op:     "delete",
		Before: before,
	}, pos, nil
}

// decodeTuple decodes a tuple of column values from the binary stream.
func (d *pgOutputDecoder) decodeTuple(data []byte, columns []colMeta) (map[string]any, int, error) {
	if len(data) < 2 {
		return nil, 0, fmt.Errorf("cdc: tuple too short: %d bytes", len(data))
	}

	colCount := int(binary.BigEndian.Uint16(data[0:2]))
	pos := 2

	result := make(map[string]any, colCount)

	for i := 0; i < colCount && i < len(columns); i++ {
		if pos >= len(data) {
			break
		}

		col := columns[i]

		switch data[pos] {
		case 'n':
			result[col.name] = nil
			pos++

		case 'u':
			result[col.name] = nil
			pos++

		case 't':
			pos++
			if pos+4 > len(data) {
				return result, pos, nil
			}
			valLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
			pos += 4
			if pos+valLen > len(data) {
				return result, pos, nil
			}
			result[col.name] = string(data[pos : pos+valLen])
			pos += valLen

		default:
			return result, pos, fmt.Errorf("cdc: unexpected tuple marker %c at column %d", data[pos], i)
		}
	}

	return result, pos, nil
}
