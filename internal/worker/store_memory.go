package worker

import (
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrJobNotFound      = errors.New("job not found")
	ErrLockNotHeld      = errors.New("lock not held by this worker")
	ErrJobAlreadyLocked = errors.New("job already locked")
)

type lockInfo struct {
	workerID  string
	expiresAt time.Time
}

// MemoryStore is an in-memory implementation of JobStore for testing and development
type MemoryStore struct {
	mu    sync.RWMutex
	jobs  map[string]*Job
	locks map[string]*lockInfo
}

// NewMemoryStore creates a new in-memory job store
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		jobs:  make(map[string]*Job),
		locks: make(map[string]*lockInfo),
	}
}

func (s *MemoryStore) Create(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job.ID == "" {
		return errors.New("job ID is required")
	}
	job.CreatedAt = time.Now()
	job.UpdatedAt = time.Now()

	// Deep copy to avoid external mutations
	jobCopy := *job
	if job.Payload != nil {
		jobCopy.Payload = make(map[string]interface{})
		for k, v := range job.Payload {
			jobCopy.Payload[k] = v
		}
	}
	s.jobs[job.ID] = &jobCopy
	return nil
}

func (s *MemoryStore) Get(id string) (*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	job, exists := s.jobs[id]
	if !exists {
		return nil, ErrJobNotFound
	}

	// Return a copy to prevent external mutations
	jobCopy := *job
	if job.Payload != nil {
		jobCopy.Payload = make(map[string]interface{})
		for k, v := range job.Payload {
			jobCopy.Payload[k] = v
		}
	}
	return &jobCopy, nil
}

func (s *MemoryStore) Update(job *Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[job.ID]; !exists {
		return ErrJobNotFound
	}

	job.UpdatedAt = time.Now()
	jobCopy := *job
	if job.Payload != nil {
		jobCopy.Payload = make(map[string]interface{})
		for k, v := range job.Payload {
			jobCopy.Payload[k] = v
		}
	}
	s.jobs[job.ID] = &jobCopy
	return nil
}

func (s *MemoryStore) ListPending(limit int) ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var pending []*Job
	now := time.Now()

	for _, job := range s.jobs {
		if job.Status == JobStatusPending && !job.ScheduledAt.After(now) {
			jobCopy := *job
			if job.Payload != nil {
				jobCopy.Payload = make(map[string]interface{})
				for k, v := range job.Payload {
					jobCopy.Payload[k] = v
				}
			}
			pending = append(pending, &jobCopy)
		}
	}

	// Sort by scheduled time (oldest first)
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].ScheduledAt.Before(pending[j].ScheduledAt)
	})

	if len(pending) > limit {
		pending = pending[:limit]
	}

	return pending, nil
}

func (s *MemoryStore) ListDeadLetter() ([]*Job, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var deadLetters []*Job
	for _, job := range s.jobs {
		if job.Status == JobStatusDeadLetter {
			jobCopy := *job
			if job.Payload != nil {
				jobCopy.Payload = make(map[string]interface{})
				for k, v := range job.Payload {
					jobCopy.Payload[k] = v
				}
			}
			deadLetters = append(deadLetters, &jobCopy)
		}
	}

	return deadLetters, nil
}

func (s *MemoryStore) AcquireLock(jobID string, workerID string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clean up expired locks
	if lock, exists := s.locks[jobID]; exists {
		if time.Now().After(lock.expiresAt) {
			delete(s.locks, jobID)
		} else if lock.workerID != workerID {
			return false, nil
		}
	}

	// Acquire or renew lock
	s.locks[jobID] = &lockInfo{
		workerID:  workerID,
		expiresAt: time.Now().Add(ttl),
	}
	return true, nil
}

func (s *MemoryStore) ReleaseLock(jobID string, workerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	lock, exists := s.locks[jobID]
	if !exists {
		return nil // Already released
	}

	if lock.workerID != workerID {
		return ErrLockNotHeld
	}

	delete(s.locks, jobID)
	return nil
}

