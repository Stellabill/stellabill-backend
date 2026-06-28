package outbox

import (
	"context"
	"fmt"
	"log"
	"math"
	"sort"
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

// dispatchLoop is intentionally disabled.
// This dispatcher is designed to be fully per-publisher to ensure one
// misbehaving publisher cannot stall others.
func (d *dispatcher) dispatchLoop() {
	// Disabled: dispatcher is per-publisher only.
	// Keep this method for backward compatibility with any potential callers.
	defer d.wg.Done()
	<-d.ctx.Done()
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

	events, err := d.repository.GetPendingEventsForPublisher(name, d.config.BatchSize)
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

				// Permanent errors go straight to dead-letter; no retry, no backoff.
				if IsPermanentPublishError(err) {
					errorMsg := err.Error()
					_ = d.repository.UpdateStatus(event.ID, StatusFailed, &errorMsg)
					continue
				}

				// update failure/backoff (per publisher)
				d.mu.Lock()
				d.publisherFailCount[name]++
				failCount := d.publisherFailCount[name]
				d.mu.Unlock()

				// bounded retry per publisher failure streak
				if failCount >= d.config.MaxRetries {
					// Mark the event as failed to stop endless retry in pending drain.
					errorMsg := err.Error()
					_ = d.repository.UpdateStatus(event.ID, StatusFailed, &errorMsg)
					// reset backoff state so we don't stall permanently
					d.mu.Lock()
					d.publisherFailCount[name] = 0
					d.publisherNextAttempt[name] = time.Time{}
					d.mu.Unlock()
					continue
				}

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

				continue
			}

			// on success reset failure count and next attempt
			d.mu.Lock()
			d.publisherFailCount[name] = 0
			d.publisherNextAttempt[name] = time.Time{}
			d.mu.Unlock()

			// Success: atomically acknowledge this publisher's high-water mark.
			if err := d.repository.MarkPublished(name, event, d.publisherNames()); err != nil {
				log.Printf("Failed to mark event %s published for %s: %v", event.ID, name, err)
				continue
			}

			// emit lag metric if available
			if !event.OccurredAt.IsZero() {
				if OutboxPublisherLag != nil {
					lag := time.Since(event.OccurredAt).Seconds()
					OutboxPublisherLag.WithLabelValues(name).Set(lag)
				}
			}

		case <-ctx.Done():
			cancel()
			log.Printf("Publisher %s processing timeout for event %s", name, event.ID)
		}
	}
}

func (d *dispatcher) publisherNames() []string {
	names := make([]string, 0, len(d.publisherMap))
	for name := range d.publisherMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// processPendingEvents processes a batch of pending events
// Disabled: dispatcher uses per-publisher drains.
func (d *dispatcher) processPendingEvents() {
	events, err := d.repository.GetPendingEvents(d.config.BatchSize)
	if err != nil {
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to get pending events: %v", err)))
		return
	}

	if len(events) == 0 {
		return
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
	if err := d.repository.MarkAsProcessing(event.ID); err != nil {
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to mark event %s as processing: %v", security.MaskPII(event.ID.String()), err)))
		return err
	}

	ctx, cancel := context.WithTimeout(d.ctx, d.config.ProcessingTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- d.publisher.Publish(ctx, event)
	}()

	select {
	case err := <-done:
		if err != nil {
			return d.handlePublishError(event, err)
		}

		if err := d.repository.UpdateStatus(event.ID, StatusCompleted, nil); err != nil {
			log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to mark event %s as completed: %v", security.MaskPII(event.ID.String()), err)))
			return err
		}

		log.Printf("%s", security.MaskPII(fmt.Sprintf("Successfully published event %s", security.MaskPII(event.ID.String()))))
		return nil

	case <-ctx.Done():
		timeoutErr := "processing timeout"
		return d.handlePublishError(event, &TimeoutError{msg: timeoutErr})
	}
}

// handlePublishError handles publishing errors and implements retry logic
func (d *dispatcher) handlePublishError(event *Event, err error) error {
	if IsPermanentPublishError(err) {
		errorMsg := err.Error()
		if updateErr := d.repository.UpdateStatus(event.ID, StatusFailed, &errorMsg); updateErr != nil {
			log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to mark event %s as failed: %v", security.MaskPII(event.ID.String()), updateErr)))
			return updateErr
		}
		log.Printf("%s", security.MaskPII(fmt.Sprintf("Event %s routed to dead-letter (permanent): %v", security.MaskPII(event.ID.String()), err)))
		return err
	}

	event.RetryCount++

	if event.RetryCount >= d.config.MaxRetries {
		errorMsg := err.Error()
		if updateErr := d.repository.UpdateStatus(event.ID, StatusFailed, &errorMsg); updateErr != nil {
			log.Printf("%s", security.MaskPII(fmt.Sprintf("Failed to mark event %s as failed: %v", security.MaskPII(event.ID.String()), updateErr)))
			return updateErr
		}

		log.Printf("%s", security.MaskPII(fmt.Sprintf("Event %s failed after %d retries: %v", security.MaskPII(event.ID.String()), event.RetryCount, err)))
		return err
	}

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
