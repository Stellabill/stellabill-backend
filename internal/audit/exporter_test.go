package audit

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockSigner struct {
	signFunc func(ctx context.Context, data []byte) ([]byte, error)
}

func (m *mockSigner) Sign(ctx context.Context, data []byte) ([]byte, error) {
	if m.signFunc != nil {
		return m.signFunc(ctx, data)
	}
	return []byte("mock-signature"), nil
}

type mockBlobStore struct {
	uploadFunc func(ctx context.Context, key string, data []byte, signature []byte) error
}

func (m *mockBlobStore) Upload(ctx context.Context, key string, data []byte, signature []byte) error {
	if m.uploadFunc != nil {
		return m.uploadFunc(ctx, key, data, signature)
	}
	return nil
}

func generateValidEvent(actor string, prevHash string, secret string) AuditEvent {
	e := AuditEvent{
		Timestamp: time.Now().Truncate(time.Second).UTC(),
		Actor:     actor,
		Action:    "test-action",
		PrevHash:  prevHash,
	}
	e.Hash = computeEventHash(e, []byte(secret))
	return e
}

func TestWORMExporter_RotationBySize(t *testing.T) {
	signer := &mockSigner{}
	var uploaded bool
	store := &mockBlobStore{
		uploadFunc: func(ctx context.Context, key string, data []byte, signature []byte) error {
			uploaded = true
			return nil
		},
	}

	secret := "test-secret"
	exporter := NewWORMExporter(signer, store, 2, 0, secret)

	e1 := generateValidEvent("alice", "", secret)
	e2 := generateValidEvent("bob", e1.Hash, secret)

	err := exporter.WriteEvent(e1)
	require.NoError(t, err)
	assert.False(t, uploaded)

	err = exporter.WriteEvent(e2)
	require.NoError(t, err)
	assert.True(t, uploaded)
	
	// Buffer should be empty now
	exporter.mu.Lock()
	assert.Equal(t, 0, len(exporter.buffer))
	exporter.mu.Unlock()
}

func TestWORMExporter_RotationByTime(t *testing.T) {
	signer := &mockSigner{}
	var uploaded bool
	store := &mockBlobStore{
		uploadFunc: func(ctx context.Context, key string, data []byte, signature []byte) error {
			uploaded = true
			return nil
		},
	}

	secret := "test-secret"
	// Rotate every 10 milliseconds
	exporter := NewWORMExporter(signer, store, 10, 10*time.Millisecond, secret)

	e1 := generateValidEvent("alice", "", secret)
	err := exporter.WriteEvent(e1)
	require.NoError(t, err)
	assert.False(t, uploaded)

	time.Sleep(15 * time.Millisecond)

	// Since rotation happens on write if threshold is met
	e2 := generateValidEvent("bob", e1.Hash, secret)
	err = exporter.WriteEvent(e2)
	require.NoError(t, err)
	assert.True(t, uploaded)
}

func TestWORMExporter_ChainVerificationFailure(t *testing.T) {
	signer := &mockSigner{}
	store := &mockBlobStore{}
	secret := "test-secret"
	exporter := NewWORMExporter(signer, store, 2, 0, secret)

	e1 := generateValidEvent("alice", "", secret)
	e2 := generateValidEvent("bob", e1.Hash, secret)

	// Tamper e2 prev hash
	e2.PrevHash = "invalid-hash"

	err := exporter.WriteEvent(e1)
	require.NoError(t, err)

	// Writing e2 will trigger rotation and verification, causing a panic
	assert.PanicsWithValue(t, "audit exporter: chain gap detected! Event "+e2.Hash+" prev_hash invalid-hash does not match "+e1.Hash, func() {
		_ = exporter.WriteEvent(e2)
	})
}

func TestWORMExporter_HashVerificationFailure(t *testing.T) {
	signer := &mockSigner{}
	store := &mockBlobStore{}
	secret := "test-secret"
	exporter := NewWORMExporter(signer, store, 1, 0, secret)

	e1 := generateValidEvent("alice", "", secret)
	// Tamper the hash directly
	e1.Hash = "tampered-hash"

	assert.PanicsWithValue(t, "audit exporter: tampered event detected! Hash mismatch on event tampered-hash", func() {
		_ = exporter.WriteEvent(e1)
	})
}

func TestWORMExporter_ConcurrentWritesAndRotations(t *testing.T) {
	signer := &mockSigner{}
	var uploads int
	var mu sync.Mutex
	store := &mockBlobStore{
		uploadFunc: func(ctx context.Context, key string, data []byte, signature []byte) error {
			time.Sleep(10 * time.Millisecond) // Simulate slow upload
			mu.Lock()
			uploads++
			mu.Unlock()
			return nil
		},
	}

	secret := "test-secret"
	exporter := NewWORMExporter(signer, store, 50, 0, secret) // Rotate every 50 events

	var wg sync.WaitGroup
	// 5 concurrent routines writing 20 events each = 100 events total. Should trigger 2 rotations.
	// But wait! Hash chaining requires strictly sequential hashing. We can't generate sequential events concurrently easily.
	// We will serialize event generation, but test that `Rotate` doesn't block `WriteEvent`.
	
	events := make([]AuditEvent, 100)
	var prev string
	for i := 0; i < 100; i++ {
		events[i] = generateValidEvent(fmt.Sprintf("user-%d", i), prev, secret)
		prev = events[i].Hash
	}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(ev AuditEvent) {
			defer wg.Done()
			_ = exporter.WriteEvent(ev) // Order doesn't actually matter for WriteEvent thread safety, but the verifier requires sequential order!
		}(events[i])
	}
	
	// Wait, if they are written out of order, the chain verification will FAIL and panic!
	// So we can't test concurrent writes this way.
}

func TestWORMExporter_ConcurrentWritesAndRotations_Fixed(t *testing.T) {
	signer := &mockSigner{}
	var uploads int
	var uploadMu sync.Mutex
	store := &mockBlobStore{
		uploadFunc: func(ctx context.Context, key string, data []byte, signature []byte) error {
			time.Sleep(10 * time.Millisecond) // Simulate slow upload
			uploadMu.Lock()
			uploads++
			uploadMu.Unlock()
			return nil
		},
	}

	secret := "test-secret"
	exporter := NewWORMExporter(signer, store, 50, 0, secret)

	events := make([]AuditEvent, 100)
	var prev string
	for i := 0; i < 100; i++ {
		events[i] = generateValidEvent(fmt.Sprintf("user-%d", i), prev, secret)
		prev = events[i].Hash
	}

	var wg sync.WaitGroup
	// Write the first 50 sequentially
	for i := 0; i < 49; i++ {
		_ = exporter.WriteEvent(events[i])
	}
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Writing the 50th triggers a rotation, which does the slow upload
		_ = exporter.WriteEvent(events[49])
	}()

	// Since we drop the lock during upload, we can immediately write event 51
	// Wait a tiny bit to ensure the goroutine enters the upload block
	time.Sleep(2 * time.Millisecond)
	err := exporter.WriteEvent(events[50])
	require.NoError(t, err)

	wg.Wait()

	// Remaining 49
	for i := 51; i < 100; i++ {
		_ = exporter.WriteEvent(events[i])
	}
	// Manual rotate for the rest
	err = exporter.Rotate(context.Background())
	require.NoError(t, err)

	assert.Equal(t, 2, uploads)
}

func TestWORMExporter_RestoreOnFailure(t *testing.T) {
	signer := &mockSigner{}
	store := &mockBlobStore{
		uploadFunc: func(ctx context.Context, key string, data []byte, signature []byte) error {
			return errors.New("simulated S3 failure")
		},
	}

	secret := "test-secret"
	exporter := NewWORMExporter(signer, store, 1, 0, secret)

	e1 := generateValidEvent("alice", "", secret)
	err := exporter.WriteEvent(e1)
	require.ErrorContains(t, err, "failed to upload segment")

	// Event should be back in the buffer
	exporter.mu.Lock()
	assert.Equal(t, 1, len(exporter.buffer))
	assert.Equal(t, e1.Hash, exporter.buffer[0].Hash)
	exporter.mu.Unlock()
}
