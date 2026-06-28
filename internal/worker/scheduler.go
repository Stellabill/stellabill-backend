package worker

import (
	"fmt"
	"sync"
	"time"

	"stellarbill-backend/internal/timeutil"
)

// Scheduler provides utilities for creating and scheduling billing jobs with
// priority-aware weighted round-robin lane selection.
type Scheduler struct {
	store       JobStore
	counter     int64
	mu          sync.Mutex

	weights         map[Priority]int
	totalWeight     int

	// starvationCount tracks consecutive high/normal picks to guard the low lane.
	starvationCount  int
	starvationLimit  int
}

// NewScheduler creates a new job scheduler with the default lane weights.
func NewScheduler(store JobStore) *Scheduler {
	s := &Scheduler{
		store:           store,
		weights:         make(map[Priority]int),
		starvationLimit: 10,
	}
	for k, v := range DefaultLaneWeights {
		s.weights[k] = v
	}
	s.recalcWeight()
	return s
}

// SetWeights replaces the lane weights. The caller should ensure each lane
// in laneOrder has a positive weight.
func (s *Scheduler) SetWeights(w map[Priority]int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.weights = make(map[Priority]int)
	for k, v := range w {
		s.weights[k] = v
	}
	s.recalcWeight()
}

func (s *Scheduler) recalcWeight() {
	n := 0
	for _, w := range s.weights {
		n += w
	}
	s.totalWeight = n
}

// SetStarvationLimit controls how many consecutive high/normal picks are
// allowed before the scheduler forces a low-priority pick.
func (s *Scheduler) SetStarvationLimit(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starvationLimit = n
}

// Next selects the next job using weighted round-robin across priority lanes.
// If the weighted lane is empty it falls back to strict priority order.
// Returns nil when no pending jobs exist.
func (s *Scheduler) Next() (*Job, error) {
	s.mu.Lock()
	s.counter++
	tw := s.totalWeight
	if tw == 0 {
		tw = 1
	}
	idx := int((s.counter - 1) % int64(tw))
	forceLow := s.starvationCount >= s.starvationLimit
	lane := s.laneForIdx(idx, forceLow)
	s.mu.Unlock()

	job, err := s.tryLane(lane)
	if err != nil {
		return nil, err
	}
	if job != nil {
		s.mu.Lock()
		if lane != PriorityLow {
			s.starvationCount++
		} else {
			s.starvationCount = 0
		}
		s.mu.Unlock()
		return job, nil
	}

	return s.pickHighestPriority()
}

// tryLane attempts to fetch a single job from the given lane.
func (s *Scheduler) tryLane(lane Priority) (*Job, error) {
	jobs, err := s.store.ListPendingByPriority(lane, 1)
	if err != nil {
		return nil, err
	}
	if len(jobs) > 0 {
		return jobs[0], nil
	}
	return nil, nil
}

// pickHighestPriority returns the oldest job from the highest non-empty lane.
func (s *Scheduler) pickHighestPriority() (*Job, error) {
	for _, p := range laneOrder {
		jobs, err := s.store.ListPendingByPriority(p, 1)
		if err != nil {
			return nil, err
		}
		if len(jobs) > 0 {
			return jobs[0], nil
		}
	}
	return nil, nil
}

// laneForIdx maps a weighted-round-robin index to a priority lane.
// When forceLow is true the low lane is always returned regardless of index.
func (s *Scheduler) laneForIdx(idx int, forceLow bool) Priority {
	if forceLow {
		return PriorityLow
	}
	cumulative := 0
	for _, p := range laneOrder {
		cumulative += s.weights[p]
		if idx < cumulative {
			return p
		}
	}
	return PriorityNormal
}

// jobBase returns fields common to every scheduled job.
func (s *Scheduler) jobBase(jobType, subscriptionID string, scheduledAt time.Time, maxAttempts int, priority Priority) *Job {
	return &Job{
		ID:             generateJobID(jobType),
		SubscriptionID: subscriptionID,
		Type:           jobType,
		Status:         JobStatusPending,
		Priority:       priority,
		ScheduledAt:    timeutil.NormalizeUTC(scheduledAt),
		MaxAttempts:    maxAttempts,
		Attempts:       0,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
}

// ScheduleCharge creates a charge job for a subscription.
func (s *Scheduler) ScheduleCharge(subscriptionID string, scheduledAt time.Time, maxAttempts int, priority Priority) (*Job, error) {
	job := s.jobBase("charge", subscriptionID, scheduledAt, maxAttempts, priority)

	if err := s.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to schedule charge: %w", err)
	}

	return job, nil
}

// ScheduleInvoice creates an invoice generation job.
func (s *Scheduler) ScheduleInvoice(subscriptionID string, scheduledAt time.Time, maxAttempts int, priority Priority) (*Job, error) {
	job := s.jobBase("invoice", subscriptionID, scheduledAt, maxAttempts, priority)

	if err := s.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to schedule invoice: %w", err)
	}

	return job, nil
}

// ScheduleReminder creates a payment reminder job.
func (s *Scheduler) ScheduleReminder(subscriptionID string, scheduledAt time.Time, maxAttempts int, priority Priority) (*Job, error) {
	job := s.jobBase("reminder", subscriptionID, scheduledAt, maxAttempts, priority)

	if err := s.store.Create(job); err != nil {
		return nil, fmt.Errorf("failed to schedule reminder: %w", err)
	}

	return job, nil
}

func generateJobID(jobType string) string {
	return fmt.Sprintf("%s-%d", jobType, timeutil.NowUTC().UnixNano())
}
