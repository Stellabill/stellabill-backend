package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type Signer interface {
	Sign(ctx context.Context, data []byte) ([]byte, error)
}

type BlobStore interface {
	Upload(ctx context.Context, key string, data []byte, signature []byte) error
}

type WORMExporter struct {
	mu         sync.Mutex
	buffer     []AuditEvent
	signer     Signer
	store      BlobStore
	maxSize    int
	rotateTime time.Duration
	secret     []byte

	lastExport time.Time
}

func NewWORMExporter(signer Signer, store BlobStore, maxSize int, rotateTime time.Duration, secret string) *WORMExporter {
	if secret == "" {
		secret = "default-stellabill-internal-secret"
	}
	return &WORMExporter{
		signer:     signer,
		store:      store,
		maxSize:    maxSize,
		rotateTime: rotateTime,
		secret:     []byte(secret),
		lastExport: time.Now(),
		buffer:     make([]AuditEvent, 0, maxSize),
	}
}

func (e *WORMExporter) WriteEvent(event AuditEvent) error {
	e.mu.Lock()
	e.buffer = append(e.buffer, event)
	shouldRotate := e.shouldRotate()
	e.mu.Unlock()

	if shouldRotate {
		return e.Rotate(context.Background())
	}
	return nil
}

func (e *WORMExporter) Rotate(ctx context.Context) error {
	e.mu.Lock()
	if len(e.buffer) == 0 {
		e.mu.Unlock()
		return nil
	}

	events := e.buffer
	e.buffer = make([]AuditEvent, 0, e.maxSize)
	e.lastExport = time.Now()
	e.mu.Unlock()

	// Verification
	for i, ev := range events {
		expectedHash := computeEventHash(ev, e.secret)
		if ev.Hash != expectedHash {
			panic(fmt.Sprintf("audit exporter: tampered event detected! Hash mismatch on event %s", ev.Hash))
		}
		if i > 0 {
			if ev.PrevHash != events[i-1].Hash {
				panic(fmt.Sprintf("audit exporter: chain gap detected! Event %s prev_hash %s does not match %s", ev.Hash, ev.PrevHash, events[i-1].Hash))
			}
		}
	}

	payload, err := json.Marshal(events)
	if err != nil {
		return e.restoreAndError(events, fmt.Errorf("failed to marshal events: %w", err))
	}

	sig, err := e.signer.Sign(ctx, payload)
	if err != nil {
		return e.restoreAndError(events, fmt.Errorf("failed to sign segment: %w", err))
	}

	key := fmt.Sprintf("audit-%d-%d.json", events[0].Timestamp.UnixNano(), events[len(events)-1].Timestamp.UnixNano())

	if err := e.store.Upload(ctx, key, payload, sig); err != nil {
		return e.restoreAndError(events, fmt.Errorf("failed to upload segment: %w", err))
	}

	return nil
}

func (e *WORMExporter) restoreAndError(events []AuditEvent, err error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Prepend events back to buffer so they aren't lost
	e.buffer = append(events, e.buffer...)
	return err
}

func (e *WORMExporter) shouldRotate() bool {
	if e.maxSize > 0 && len(e.buffer) >= e.maxSize {
		return true
	}
	if e.rotateTime > 0 && time.Since(e.lastExport) >= e.rotateTime {
		return true
	}
	return false
}
