package outbox

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"stellarbill-backend/internal/security"
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
	repository   Repository
	publisher    Publisher
	publisherMap map[string]Publisher
	config       DispatcherConfig

	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	running bool
	mu      sync.RWMutex
	// per-publisher failure/backoff state
	publisherFailCount   map[string]int
	publisherNextAttempt map[string]time.Time
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

	// Ensure publisher progress table exists
	if err := d.repository.EnsurePublisherProgressTable(); err != nil {
		return err
	}

	// Build publisher map (support multi publisher)
	d.publisherMap = make(map[string]Publisher)
	d.publisherFailCount = make(map[string]int)
	d.publisherNextAttempt = make(map[string]time.Time)
	switch p := d.publisher.(type) {
	case *MultiPublisher:
		for i, child := range p.publishers {
			name := fmt.Sprintf("publisher-%d", i)
			d.publisherMap[name] = child
		}
	case *ConsolePublisher:
		d.publisherMap["console"] = p
	case *HTTPPublisher:
		d.publisherMap["http"] = p
	default:
		d.publisherMap["default"] = d.publisher
	}

	// Start per-publisher drain goroutines
	for name, pub := range d.publisherMap {
		d.wg.Add(1)
		go d.publisherDrain(name, pub)
	}

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

	log.Printf("%s", security.MaskPII("Outbox dispatcher stopped"))
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

// publisherDrain processes events for a single publisher using its own cursor
func (d *dispatcher) publisherDrain(name string, pub Publisher) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.drainOnceForPublisher(name, pub)
		}
	}
}

func (d *dispatcher) drainOnceForPublisher(name string, pub Publisher) {
	// Respect backoff for this publisher
	d.mu.RLock()
	next := d.publisherNextAttempt[name]
	d.mu.RUnlock()
	if !next.IsZero() && time.Now().Before(next) {
		return
	}

	// Get last progress
	since, lastID, err := d.repository.GetPublisherProgress(name)
	if err != nil {
		log.Printf("Failed to get publisher progress for %s: %v", name, err)
		return
	}

	events, err := d.repository.GetPendingEventsSince(since, lastID, d.config.BatchSize)
	if err != nil {
		log.Printf("Failed to get pending events for publisher %s: %v", name, err)
		return
	}

	for _, event := range events {
		// Publish with timeout
		ctx, cancel := context.WithTimeout(d.ctx, d.config.ProcessingTimeout)
		errCh := make(chan error, 1)
		go func(ev *Event) { errCh <- pub.Publish(ctx, ev) }(event)

		select {
		case err := <-errCh:
			cancel()
			if err != nil {
				log.Printf("Publisher %s failed for event %s: %v", name, event.ID, err)
				// update failure/backoff
				d.mu.Lock()
				d.publisherFailCount[name]++
				failCount := d.publisherFailCount[name]
				d.mu.Unlock()

				// exponential backoff based on failCount, capped
				backoff := math.Pow(d.config.RetryBackoffFactor, float64(failCount))
				if backoff < 1 {
					backoff = 1
				}
				if backoff > 3600 {
					backoff = 3600
				}
				nextAttempt := time.Now().Add(time.Duration(backoff) * time.Second)
				d.mu.Lock()
				d.publisherNextAttempt[name] = nextAttempt
				d.mu.Unlock()

				// Do not advance cursor; will retry after backoff
				continue
			}

			// on success reset failure count and next attempt
			d.mu.Lock()
			d.publisherFailCount[name] = 0
			d.publisherNextAttempt[name] = time.Time{}
			d.mu.Unlock()

			// Success: advance publisher cursor
			if err := d.repository.UpdatePublisherProgress(name, event.OccurredAt, event.ID); err != nil {
				log.Printf("Failed to update publisher progress for %s: %v", name, err)
				continue
			}

			// emit lag metric if available
			if event.OccurredAt.IsZero() == false {
				// set gauge: now - last processed (after update)
				if OutboxPublisherLag != nil {
					lag := time.Since(event.OccurredAt).Seconds()
					OutboxPublisherLag.WithLabelValues(name).Set(lag)
				}
			}

			// If all publishers have processed this event, mark it completed
			all, err := d.allPublishersProcessed(event)
			if err != nil {
				log.Printf("Failed to check all publishers progress for event %s: %v", event.ID, err)
				continue
			}
			if all {
				if err := d.repository.UpdateStatus(event.ID, StatusCompleted, nil); err != nil {
					log.Printf("Failed to mark event %s as completed: %v", event.ID, err)
				}
			}

		case <-ctx.Done():
			cancel()
			log.Printf("Publisher %s processing timeout for event %s", name, event.ID)
			// don't advance cursor
		}
	}
}

// allPublishersProcessed checks whether every registered publisher has progressed past the event
func (d *dispatcher) allPublishersProcessed(event *Event) (bool, error) {
	for name := range d.publisherMap {
		since, lastID, err := d.repository.GetPublisherProgress(name)
		if err != nil {
			return false, err
		}
		if since == nil {
			return false, nil
		}
		// If last processed occurred_at < event.OccurredAt, not yet processed
		if since.Before(event.OccurredAt) {
			return false, nil
		}
		// If equal timestamps, ensure lastID > event.ID
		if since.Equal(event.OccurredAt) {
			if lastID == nil || lastID.String() < event.ID.String() {
				return false, nil
			}
		}
	}
	return true, nil
}

// processPendingEvents processes a batch of pending events
func (d *dispatcher) processPendingEvents() {
	events, err := d.repository.GetPendingEvents(d.config.BatchSize)
	if err != nil {
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to get pending events: %v", err)))
		return
	}

	if len(events) == 0 {
		return // No events to process
	}

	log.Printf("%s", security.MaskPII(fmt.Sprintf("Processing %d pending events", len(events))))

	for _, event := range events {
		if err := d.processEvent(event); err != nil {
			log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to process event %s: %v", security.MaskPII(event.ID.String()), err)))
		}
	}
}

// processEvent processes a single event
func (d *dispatcher) processEvent(event *Event) error {
	// Mark as processing to prevent other dispatchers from picking it up
	if err := d.repository.MarkAsProcessing(event.ID); err != nil {
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to mark event %s as processing: %v", security.MaskPII(event.ID.String()), err)))
		return err
	}

	// Create a timeout context for processing
	ctx, cancel := context.WithTimeout(d.ctx, d.config.ProcessingTimeout)
	defer cancel()

	// Process in a goroutine to respect timeout
	done := make(chan error, 1)
	go func() {
		done <- d.publisher.Publish(ctx, event)
	}()

	select {
	case err := <-done:
		if err != nil {
			return d.handlePublishError(event, err)
		}

		// Mark as completed
		if err := d.repository.UpdateStatus(event.ID, StatusCompleted, nil); err != nil {
			log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to mark event %s as completed: %v", security.MaskPII(event.ID.String()), err)))
			return err
		}

		log.Printf("%s", security.MaskPII(fmt.Sprintf("Successfully published event %s", security.MaskPII(event.ID.String()))))
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
			log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to mark event %s as failed: %v", security.MaskPII(event.ID.String()), updateErr)))
			return updateErr
		}

		log.Printf("%s", security.MaskPII(fmt.Sprintf("Event %s failed after %d retries: %v", security.MaskPII(event.ID.String()), event.RetryCount, err)))
		return err
	}

	// Calculate next retry time with exponential backoff
	backoffSeconds := math.Pow(d.config.RetryBackoffFactor, float64(event.RetryCount))
	nextRetryAt := time.Now().Add(time.Duration(backoffSeconds) * time.Second)

	errorMsg := err.Error()
	if updateErr := d.repository.IncrementRetryCount(event.ID, nextRetryAt, &errorMsg); updateErr != nil {
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to increment retry count for event %s: %v", security.MaskPII(event.ID.String()), updateErr)))
		return updateErr
	}

	log.Printf("%s", security.MaskPII(fmt.Sprintf("Event %s retry %d scheduled for %v: %v", security.MaskPII(event.ID.String()), event.RetryCount, nextRetryAt, err)))
	return err
}

// cleanupCompletedEvents removes old completed events
func (d *dispatcher) cleanupCompletedEvents() {
	cutoff := time.Now().Add(-d.config.CompletedEventTTL)
	deleted, err := d.repository.DeleteCompletedEvents(cutoff)
	if err != nil {
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to cleanup completed events: %v", err)))
		return
	}

	if deleted > 0 {
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Cleaned up %d completed events older than %v", deleted, cutoff)))
	}
}

// TimeoutError represents a processing timeout error
type TimeoutError struct {
	msg string
}

func (e *TimeoutError) Error() string {
	return e.msg
}
