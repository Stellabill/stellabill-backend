package reconciliation

import (
	"context"
	"fmt"
	"time"

)

// Service provides reconciliation operations.
type Service struct {
	Adapter Adapter
	Store   Store
}

// NewService creates a new reconciliation service.
func NewService(adapter Adapter, store Store) *Service {
	return &Service{
		Adapter: adapter,
		Store:   store,
	}
}

// Reconcile performs reconciliation for a list of backend subscriptions.
// It returns the reconciliation reports and an error if any occurred during the process.
// It implements deterministic retry logic with exponential backoff for transient failures.
func (s *Service) Reconcile(ctx context.Context, backendSubs []BackendSubscription, opts ...RetryOption) ([]Report, error) {
	// Apply default options
	options := &RetryOptions{
		MaxAttempts: 3,
		BaseDelay:   1 * time.Second,
		MaxDelay:    30 * time.Second,
	}
	
	// Apply custom options
	for _, opt := range opts {
		opt(options)
	}
	
	var lastErr error
	
	for attempt := 0; attempt < options.MaxAttempts; attempt++ {
		// Try to fetch snapshots
		snaps, err := s.Adapter.FetchSnapshots(ctx)
		if err != nil {
			lastErr = err
			ReconciliationTotal.WithLabelValues("error").Inc()
			
			// If this isn't the last attempt, wait before retrying
			if attempt < options.MaxAttempts-1 {
				delay := options.BaseDelay * time.Duration(1<<uint(attempt)) // exponential backoff
				if delay > options.MaxDelay {
					delay = options.MaxDelay
				}
				
				// Add jitter to prevent thundering herd
				jitter := time.Duration(float64(delay) * 0.1 * float64(time.Now().UnixNano()%10) / 10.0)
				delay += jitter
				
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(delay):
					// Continue to next attempt
				}
			}
			continue
		}
		
		// If we got snapshots successfully, process them
		snapMap := make(map[string]*Snapshot, len(snaps))
		for i := range snaps {
			s := snaps[i]
			snapMap[s.SubscriptionID] = &s
		}

		reconciler := New()
		reports := make([]Report, 0, len(backendSubs))
		
		for _, b := range backendSubs {
			rep := reconciler.Compare(b, snapMap[b.SubscriptionID])
			
			// Record metrics for each report
			ReconciliationReportsTotal.WithLabelValues(fmt.Sprintf("%t", rep.Matched)).Inc()
			
			// Record lag for stale snapshots
			if rep.Contract.ExportedAt != (time.Time{}) && !rep.Matched {
				for _, mismatch := range rep.Mismatches {
					if mismatch.Field == "snapshot_stale" {
						lag := b.UpdatedAt.Sub(rep.Contract.ExportedAt).Seconds()
						if lag > 0 {
							ReconciliationLag.WithLabelValues(b.SubscriptionID).Set(lag)
						}
					}
				}
			}
			
			reports = append(reports, rep)
		}
		
		// Try to save reports if store is configured
		if s.Store != nil {
			if err := s.Store.SaveReports(reports); err != nil {
				lastErr = err
				ReconciliationTotal.WithLabelValues("error").Inc()
				
				// If this isn't the last attempt, wait before retrying
				if attempt < options.MaxAttempts-1 {
					delay := options.BaseDelay * time.Duration(1<<uint(attempt)) // exponential backoff
					if delay > options.MaxDelay {
						delay = options.MaxDelay
					}
					
					// Add jitter to prevent thundering herd
					jitter := time.Duration(float64(delay) * 0.1 * float64(time.Now().UnixNano()%10) / 10.0)
					delay += jitter
					
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(delay):
						// Continue to next attempt
					}
				}
				continue
			}
		}
		
		// Success!
		ReconciliationTotal.WithLabelValues("success").Inc()
		return reports, nil
	}
	
	// All attempts failed
	return nil, lastErr
}

// RetryOptions configures the retry behavior.
type RetryOptions struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// RetryOption sets options for the retry behavior.
type RetryOption func(*RetryOptions)

// WithMaxAttempts sets the maximum number of attempts.
func WithMaxAttempts(max int) RetryOption {
	return func(o *RetryOptions) {
		o.MaxAttempts = max
	}
}

// WithBaseDelay sets the base delay between retries.
func WithBaseDelay(d time.Duration) RetryOption {
	return func(o *RetryOptions) {
		o.BaseDelay = d
	}
}

// WithMaxDelay sets the maximum delay between retries.
func WithMaxDelay(d time.Duration) RetryOption {
	return func(o *RetryOptions) {
		o.MaxDelay = d
	}
}