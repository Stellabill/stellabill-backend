package cdc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// Consumer reads the PostgreSQL replication stream and forwards decoded
// changes to the configured sinks.
type Consumer struct {
	cfg     ConsumerConfig
	running bool
	mu      sync.RWMutex
	cancel  context.CancelFunc

	// lastLSN tracks the most recent LSN we've confirmed to the server.
	lastLSN     uint64
	lastLSNLock sync.RWMutex
}

// NewConsumer creates a new CDC consumer.
func NewConsumer(cfg ConsumerConfig) (*Consumer, error) {
	if cfg.ConnString == "" {
		return nil, errors.New("cdc: connection string is required")
	}
	if cfg.SlotName == "" {
		cfg.SlotName = "stellabill_cdc_slot"
	}
	if cfg.PublicationName == "" {
		cfg.PublicationName = "stellabill_cdc"
	}
	if cfg.StandbyTimeout <= 0 {
		cfg.StandbyTimeout = 10 * time.Second
	}
	if cfg.ReconnectBackoff <= 0 {
		cfg.ReconnectBackoff = 1 * time.Second
	}
	return &Consumer{cfg: cfg}, nil
}

// Start begins consuming the replication stream. It blocks until the
// consumer is stopped or encounters a fatal error.
func (c *Consumer) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return errors.New("cdc: consumer already running")
	}
	c.running = true
	ctx, c.cancel = context.WithCancel(ctx)
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		c.mu.Unlock()
		for _, s := range c.cfg.Sinks {
			if err := s.Close(); err != nil {
				log.Printf("cdc: error closing sink: %v", err)
			}
		}
	}()

	return c.replicationLoop(ctx)
}

// Stop gracefully shuts down the consumer.
func (c *Consumer) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
	}
}

// IsRunning returns whether the consumer is actively streaming.
func (c *Consumer) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// replicationLoop manages connection lifecycle and reconnection logic.
func (c *Consumer) replicationLoop(ctx context.Context) error {
	var attempts int

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := c.runReplication(ctx)
		if err == nil {
			return nil
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}

		attempts++
		log.Printf("cdc: replication error (attempt %d): %v", attempts, err)

		if c.cfg.MaxReconnectAttempts > 0 && attempts >= c.cfg.MaxReconnectAttempts {
			return fmt.Errorf("cdc: exceeded max reconnect attempts (%d): %w",
				c.cfg.MaxReconnectAttempts, err)
		}

		backoff := time.Duration(attempts) * c.cfg.ReconnectBackoff
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		log.Printf("cdc: reconnecting in %v...", backoff)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
}

// runReplication establishes a replication connection and processes the stream.
func (c *Consumer) runReplication(ctx context.Context) error {
	conn, err := pgconn.Connect(ctx, c.cfg.ConnString)
	if err != nil {
		return fmt.Errorf("cdc: connect failed: %w", err)
	}
	defer conn.Close(ctx)

	// IDENTIFY_SYSTEM – verify replication capability.
	if err := c.execSimple(ctx, conn, "IDENTIFY_SYSTEM"); err != nil {
		return fmt.Errorf("cdc: IDENTIFY_SYSTEM failed: %w", err)
	}

	// Create the replication slot if it doesn't exist (ignore "already exists").
	createSQL := fmt.Sprintf("CREATE_REPLICATION_SLOT %s LOGICAL pgoutput", c.cfg.SlotName)
	if err := c.execSimple(ctx, conn, createSQL); err != nil {
		log.Printf("cdc: create slot skipped (may already exist): %v", err)
	}

	// START_REPLICATION – this puts the connection into CopyBoth mode.
	frontend := conn.Frontend()

	startCmd := fmt.Sprintf(
		"START_REPLICATION SLOT %s LOGICAL 0/0 (proto_version '1', publication_names '%s')",
		c.cfg.SlotName, c.cfg.PublicationName,
	)

	frontend.Send(&pgproto3.Query{String: startCmd})
	if err := frontend.Flush(); err != nil {
		return fmt.Errorf("cdc: flush START_REPLICATION: %w", err)
	}

	// Read the CopyBothResponse.
	msg, err := conn.ReceiveMessage(ctx)
	if err != nil {
		return fmt.Errorf("cdc: receive CopyBothResponse: %w", err)
	}

	if _, ok := msg.(*pgproto3.CopyBothResponse); !ok {
		return fmt.Errorf("cdc: expected CopyBothResponse, got %T", msg)
	}

	log.Printf("cdc: replication started on slot %q, publication %q",
		c.cfg.SlotName, c.cfg.PublicationName)

	decoder := newPGOutputDecoder()
	standbyDeadline := time.Now().Add(c.cfg.StandbyTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Use a context deadline so we can periodically send status updates.
		nextDeadline := time.Now().Add(c.cfg.StandbyTimeout)
		deadlineCtx, deadlineCancel := context.WithDeadline(ctx, nextDeadline)
		msg, err := conn.ReceiveMessage(deadlineCtx)
		deadlineCancel()
		if err != nil {
			// Timeout is expected – send a standby status update.
			if isTimeout(err) {
				c.sendStandbyStatusUpdate(conn)
				standbyDeadline = time.Now().Add(c.cfg.StandbyTimeout)
				continue
			}
			return fmt.Errorf("cdc: receive message: %w", err)
		}

		switch m := msg.(type) {
		case *pgproto3.CopyData:
			// Decode and forward changes.
			changes, newLSN, err := decoder.decode(m.Data)
			if err != nil {
				log.Printf("cdc: decode error: %v", err)
				continue
			}

			for _, change := range changes {
			change.LSN = newLSN
			// Write to all sinks before advancing LSN.
			// If a sink fails, log and continue but do not advance
			// past the last successfully written LSN.
			allSinksOK := true
			for _, sink := range c.cfg.Sinks {
				if err := sink.WriteChange(ctx, change); err != nil {
					log.Printf("cdc: sink write error: %v", err)
					allSinksOK = false
				}
			}
			if allSinksOK {
				c.updateLSN(newLSN)
			}
		}

		case *pgproto3.CopyDone:
			log.Printf("cdc: server sent CopyDone, restarting replication")
			return nil

		case *pgproto3.ErrorResponse:
			return fmt.Errorf("cdc: server error: %s", m.Message)

		default:
			// Ignore other message types (ParameterStatus, NoticeResponse, etc.)
		}

		// Send standby status update periodically.
		if time.Now().After(standbyDeadline) {
			c.sendStandbyStatusUpdate(conn)
			standbyDeadline = time.Now().Add(c.cfg.StandbyTimeout)
		}
	}
}

// execSimple sends a simple query to the server and consumes all responses.
func (c *Consumer) execSimple(ctx context.Context, conn *pgconn.PgConn, sql string) error {
	result := conn.Exec(ctx, sql)
	_, err := result.ReadAll()
	return err
}

// sendStandbyStatusUpdate sends a status update so the server knows
// we've consumed up to the last LSN.
func (c *Consumer) sendStandbyStatusUpdate(conn *pgconn.PgConn) {
	c.lastLSNLock.RLock()
	lsn := c.lastLSN
	c.lastLSNLock.RUnlock()

	if lsn == 0 {
		return
	}

	// Build standby status update message:
	// Byte 1:     'r' (standby status update)
	// Bytes 2-9:  WAL write LSN (8 bytes, big-endian)
	// Bytes 10-17: WAL flush LSN (8 bytes, big-endian)
	// Bytes 18-25: WAL apply LSN (8 bytes, big-endian)
	// Bytes 26-33: Timestamp (8 bytes, big-endian, microseconds since epoch)
	// Byte 34:     reply flag (0 or 1)
	data := make([]byte, 34)
	data[0] = 'r'
	binary.BigEndian.PutUint64(data[1:9], lsn)
	binary.BigEndian.PutUint64(data[9:17], lsn)
	binary.BigEndian.PutUint64(data[17:25], lsn)
	binary.BigEndian.PutUint64(data[25:33], uint64(time.Now().UnixMicro()))
	data[33] = 1 // request reply

	frontend := conn.Frontend()
	if err := frontend.SendUnbufferedEncodedCopyData(data); err != nil {
		log.Printf("cdc: standby status update failed: %v", err)
	}
}

func (c *Consumer) updateLSN(lsn uint64) {
	c.lastLSNLock.Lock()
	if lsn > c.lastLSN {
		c.lastLSN = lsn
	}
	c.lastLSNLock.Unlock()
}

// isTimeout checks if the error is a timeout.
func isTimeout(err error) bool {
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}
