package outbox

import (
	"context"
	"math"
	"sync"
	"time"

	"stellabill-backend/internal/structuredlog"
)

// DispatcherConfig holds configuration for the dispatcher
type DispatcherConfig struct {
	PollInterval        time.Duration
	BatchSize           int
	MaxRetries          int
	RetryBackoffFactor  float64
	FailureLogWindow    time.Duration
	CleanupInterval     time.Duration
	CompletedEventTTL   time.Duration
	ProcessingTimeout   time.Duration
}

// DefaultDispatcherConfig returns default configuration
func DefaultDispatcherConfig() DispatcherConfig {
	return DispatcherConfig{
		PollInterval:       5 * time.Second,
		BatchSize:          10,
		MaxRetries:         3,
		RetryBackoffFactor: 2.0,
		FailureLogWindow:   30 * time.Second,
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
	
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	running    bool
	mu         sync.RWMutex
	logger     *structuredlog.Logger
	throttler  *structuredlog.Throttler
	now        func() time.Time
}

// NewDispatcher creates a new outbox dispatcher
func NewDispatcher(repository Repository, publisher Publisher, config DispatcherConfig) Dispatcher {
	if config.FailureLogWindow <= 0 {
		config.FailureLogWindow = 30 * time.Second
	}
	return &dispatcher{
		repository: repository,
		publisher:  publisher,
		config:     config,
		logger:     defaultLogger,
		throttler:  structuredlog.NewThrottler(config.FailureLogWindow),
		now:        time.Now,
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
	
	d.logger.Info("outbox dispatcher started", outboxFields("outbox.dispatcher", "started"))
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
	
	d.logger.Info("outbox dispatcher stopped", outboxFields("outbox.dispatcher", "stopped"))
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
	started := d.now()
	events, err := d.repository.GetPendingEvents(d.config.BatchSize)
	if err != nil {
		d.logFailure("pending_events", "failed to fetch pending outbox events", err, addDuration(outboxFields("outbox.dispatcher.poll", "repository_error"), started))
		return
	}
	
	if len(events) == 0 {
		return // No events to process
	}
	
	fields := addDuration(outboxFields("outbox.dispatcher.poll", "processing"), started)
	fields["batch_size"] = len(events)
	d.logger.Info("processing pending outbox events", fields)
	
	for _, event := range events {
		if err := d.processEvent(event); err != nil {
			d.logFailure("process_event:"+event.ID.String(), "failed to process outbox event", err, structuredlog.Fields{
				structuredlog.FieldRequestID: "",
				structuredlog.FieldActor:     "system",
				structuredlog.FieldTenant:    "system",
				structuredlog.FieldRoute:     "outbox.dispatcher.process",
				structuredlog.FieldStatus:    "event_failed",
				structuredlog.FieldDuration:  0,
				"event_id":                   event.ID.String(),
				"event_type":                 event.EventType,
				"retry_count":                event.RetryCount,
			})
		}
	}
}

// processEvent processes a single event
func (d *dispatcher) processEvent(event *Event) error {
	started := d.now()

	// Mark as processing to prevent other dispatchers from picking it up
	if err := d.repository.MarkAsProcessing(event.ID); err != nil {
		d.logFailure("mark_processing:"+event.ID.String(), "failed to mark outbox event as processing", err, structuredlog.Fields{
			structuredlog.FieldRequestID: "",
			structuredlog.FieldActor:     "system",
			structuredlog.FieldTenant:    "system",
			structuredlog.FieldRoute:     "outbox.dispatcher.process",
			structuredlog.FieldStatus:    "mark_processing_failed",
			structuredlog.FieldDuration:  d.now().Sub(started).Milliseconds(),
			"event_id":                   event.ID.String(),
			"event_type":                 event.EventType,
		})
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
			d.logFailure("mark_completed:"+event.ID.String(), "failed to mark outbox event as completed", err, structuredlog.Fields{
				structuredlog.FieldRequestID: "",
				structuredlog.FieldActor:     "system",
				structuredlog.FieldTenant:    "system",
				structuredlog.FieldRoute:     "outbox.dispatcher.process",
				structuredlog.FieldStatus:    "mark_completed_failed",
				structuredlog.FieldDuration:  d.now().Sub(started).Milliseconds(),
				"event_id":                   event.ID.String(),
				"event_type":                 event.EventType,
			})
			return err
		}
		
		d.logger.Info("outbox event published", structuredlog.Fields{
			structuredlog.FieldRequestID: "",
			structuredlog.FieldActor:     "system",
			structuredlog.FieldTenant:    "system",
			structuredlog.FieldRoute:     "outbox.dispatcher.process",
			structuredlog.FieldStatus:    StatusCompleted,
			structuredlog.FieldDuration:  d.now().Sub(started).Milliseconds(),
			"event_id":                   event.ID.String(),
			"event_type":                 event.EventType,
		})
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
			d.logFailure("mark_failed:"+event.ID.String(), "failed to mark outbox event as terminally failed", updateErr, structuredlog.Fields{
				structuredlog.FieldRequestID: "",
				structuredlog.FieldActor:     "system",
				structuredlog.FieldTenant:    "system",
				structuredlog.FieldRoute:     "outbox.dispatcher.retry",
				structuredlog.FieldStatus:    "terminal_update_failed",
				structuredlog.FieldDuration:  0,
				"event_id":                   event.ID.String(),
				"event_type":                 event.EventType,
				"retry_count":                event.RetryCount,
			})
			return updateErr
		}
		
		d.logFailure("terminal:"+event.ID.String(), "outbox event exhausted retries", err, structuredlog.Fields{
			structuredlog.FieldRequestID: "",
			structuredlog.FieldActor:     "system",
			structuredlog.FieldTenant:    "system",
			structuredlog.FieldRoute:     "outbox.dispatcher.retry",
			structuredlog.FieldStatus:    StatusFailed,
			structuredlog.FieldDuration:  0,
			"event_id":                   event.ID.String(),
			"event_type":                 event.EventType,
			"retry_count":                event.RetryCount,
			"max_retries":                d.config.MaxRetries,
		})
		return err
	}
	
	// Calculate next retry time with exponential backoff
	backoffSeconds := math.Pow(d.config.RetryBackoffFactor, float64(event.RetryCount))
	nextRetryAt := d.now().Add(time.Duration(backoffSeconds) * time.Second)
	
	errorMsg := err.Error()
	if updateErr := d.repository.IncrementRetryCount(event.ID, nextRetryAt, &errorMsg); updateErr != nil {
		d.logFailure("increment_retry:"+event.ID.String(), "failed to schedule outbox retry", updateErr, structuredlog.Fields{
			structuredlog.FieldRequestID: "",
			structuredlog.FieldActor:     "system",
			structuredlog.FieldTenant:    "system",
			structuredlog.FieldRoute:     "outbox.dispatcher.retry",
			structuredlog.FieldStatus:    "retry_schedule_failed",
			structuredlog.FieldDuration:  0,
			"event_id":                   event.ID.String(),
			"event_type":                 event.EventType,
			"retry_count":                event.RetryCount,
		})
		return updateErr
	}
	
	d.logFailure("retry:"+event.EventType+":"+err.Error(), "outbox event scheduled for retry", err, structuredlog.Fields{
		structuredlog.FieldRequestID: "",
		structuredlog.FieldActor:     "system",
		structuredlog.FieldTenant:    "system",
		structuredlog.FieldRoute:     "outbox.dispatcher.retry",
		structuredlog.FieldStatus:    "retry_scheduled",
		structuredlog.FieldDuration:  0,
		"event_id":                   event.ID.String(),
		"event_type":                 event.EventType,
		"retry_count":                event.RetryCount,
		"next_retry_at":              nextRetryAt.UTC().Format(time.RFC3339Nano),
	})
	return err
}

// cleanupCompletedEvents removes old completed events
func (d *dispatcher) cleanupCompletedEvents() {
	started := d.now()
	cutoff := d.now().Add(-d.config.CompletedEventTTL)
	deleted, err := d.repository.DeleteCompletedEvents(cutoff)
	if err != nil {
		d.logFailure("cleanup_completed", "failed to cleanup completed outbox events", err, addDuration(outboxFields("outbox.dispatcher.cleanup", "cleanup_failed"), started))
		return
	}
	
	if deleted > 0 {
		fields := addDuration(outboxFields("outbox.dispatcher.cleanup", "cleanup_complete"), started)
		fields["deleted"] = deleted
		fields["cutoff"] = cutoff.UTC().Format(time.RFC3339Nano)
		d.logger.Info("cleaned up completed outbox events", fields)
	}
}

func (d *dispatcher) logFailure(key, message string, err error, fields structuredlog.Fields) {
	decision := d.throttler.Decide(key)
	if !decision.Allow {
		return
	}
	if fields == nil {
		fields = structuredlog.Fields{}
	}
	fields["error"] = err
	if decision.Suppressed > 0 {
		fields["suppressed_count"] = decision.Suppressed
	}
	d.logger.Warn(message, fields)
}

// TimeoutError represents a processing timeout error
type TimeoutError struct {
	msg string
}

func (e *TimeoutError) Error() string {
	return e.msg
}
