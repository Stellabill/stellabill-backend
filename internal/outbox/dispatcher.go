package outbox

import (
	"context"
	"log"
	"math"
	"sync"
	"time"
)

// DispatcherConfig holds configuration for the dispatcher
type DispatcherConfig struct {
	PollInterval       time.Duration
	BatchSize          int
	MaxRetries         int
	RetryBackoffFactor float64
	CleanupInterval    time.Duration
	CompletedEventTTL  time.Duration
	ProcessingTimeout  time.Duration
}

// DefaultDispatcherConfig returns default configuration
func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		PollInterval:       5 * time.Second,
		BatchSize:          10,
		MaxRetries:         3,
		RetryBackoffFactor: 2.0,
		CleanupInterval:    1 * time.Hour,
		CompletedEventTTL:  24 * time.Hour,
		ProcessingTimeout:  30 * time.Second,
	}
}

// dispatcher implements the Dispatcher interface
type dispatcher struct {
	repository Repository
	publisher  Publisher
	config     DispatcherConfig

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	mu      sync.RWMutex
}

// NewDispatcher creates a new outbox dispatcher
func NewDispatcher(repository Repository, publisher Publisher, config DispatcherConfig) Dispatcher {
	return &dispatcher{
		repository: repository,
		publisher:  publisher,
		config:     config,
	}
}

// Start starts the dispatcher
func (d *dispatcher) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return nil // Already running
	}

	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.running = true

	// Start the main dispatcher goroutine
	d.wg.Add(1)
	go d.dispatchLoop()

	// Start the cleanup goroutine
	d.wg.Add(1)
	go d.cleanupLoop()

	log.Println("Outbox dispatcher started")
	return nil
}

// Stop stops the dispatcher
func (d *dispatcher) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil // Already stopped
	}

	d.cancel()
	d.wg.Wait()
	d.running = false

	log.Println("Outbox dispatcher stopped")
	return nil
}

// IsRunning returns whether the dispatcher is running
func (d *dispatcher) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// dispatchLoop is the main processing loop
func (d *dispatcher) dispatchLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.processPendingEvents()
		}
	}
}

// cleanupLoop handles cleanup of completed events
func (d *dispatcher) cleanupLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.cleanupCompletedEvents()
		}
	}
}

// processPendingEvents processes a batch of pending events
func (d *dispatcher) processPendingEvents() {
	events, err := d.repository.GetPendingEvents(d.config.BatchSize)
	if err != nil {
		log.Printf("Failed to get pending events: %v", err)
		return
	}

	if len(events) == 0 {
		return // No events to process
	}

	log.Printf("Processing %d pending events", len(events))

	for _, event := range events {
		if err := d.processEvent(event); err != nil {
			log.Printf("Failed to process event %s: %v", event.ID, err)
		}
	}
}

// processEvent processes a single event
func (d *dispatcher) processEvent(event *Event) error {
	// Mark as processing to prevent other dispatchers from picking it up
	if err := d.repository.MarkAsProcessing(event.ID); err != nil {
		log.Printf("Failed to mark event %s as processing: %v", event.ID, err)
		return err
	}

	// Create a timeout context for processing
	ctx, cancel := context.WithTimeout(d.ctx, d.config.ProcessingTimeout)
	defer cancel()

	// Process in a goroutine to respect timeout
	done := make(chan error, 1)
	go func() {
		done <- d.publisher.Publish(event)
	}()

	select {
	case err := <-done:
		if err != nil {
			return d.handlePublishError(event, err)
		}

		// Mark as completed
		if err := d.repository.UpdateStatus(event.ID, StatusCompleted, nil); err != nil {
			log.Printf("Failed to mark event %s as completed: %v", event.ID, err)
			return err
		}

		log.Printf("Successfully published event %s", event.ID)
		return nil

	case <-ctx.Done():
		// Processing timeout
		timeoutErr := "processing timeout"
		return d.handlePublishError(event, &TimeoutError{msg: timeoutErr})
	}
}

// handlePublishError handles publishing errors and implements retry logic
func (d *dispatcher) handlePublishError(event *Event, err error) error {
	event.RetryCount++

	if event.RetryCount >= d.config.MaxRetries {
		// Max retries reached, mark as failed
		errorMsg := err.Error()
		if updateErr := d.repository.UpdateStatus(event.ID, StatusFailed, &errorMsg); updateErr != nil {
			log.Printf("Failed to mark event %s as failed: %v", event.ID, updateErr)
			return updateErr
		}

		log.Printf("Event %s failed after %d retries: %v", event.ID, event.RetryCount, err)
		return err
	}

	// Calculate next retry time with exponential backoff
	backoffSeconds := math.Pow(d.config.RetryBackoffFactor, float64(event.RetryCount))
	nextRetryAt := time.Now().Add(time.Duration(backoffSeconds) * time.Second)

	errorMsg := err.Error()
	if updateErr := d.repository.IncrementRetryCount(event.ID, nextRetryAt, &errorMsg); updateErr != nil {
		log.Printf("Failed to increment retry count for event %s: %v", event.ID, updateErr)
		return updateErr
	}

	log.Printf("Event %s retry %d scheduled for %v: %v", event.ID, event.RetryCount, nextRetryAt, err)
	return err
}

// cleanupCompletedEvents removes old completed events
func (d *dispatcher) cleanupCompletedEvents() {
	cutoff := time.Now().Add(-d.config.CompletedEventTTL)
	deleted, err := d.repository.DeleteCompletedEvents(cutoff)
	if err != nil {
		log.Printf("Failed to cleanup completed events: %v", err)
		return
	}

	if deleted > 0 {
		log.Printf("Cleaned up %d completed events older than %v", deleted, cutoff)
	}
}

// TimeoutError represents a processing timeout error
type TimeoutError struct {
	msg string
}

func (e *TimeoutError) Error() string {
	return e.msg
}

