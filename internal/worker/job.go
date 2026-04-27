package worker

import (
	"time"
)

// JobStatus represents the current state of a billing job
type JobStatus string

const (
	JobStatusPending    JobStatus = "pending"
	JobStatusRunning    JobStatus = "running"
	JobStatusCompleted  JobStatus = "completed"
	JobStatusFailed     JobStatus = "failed"
	JobStatusDeadLetter JobStatus = "dead_letter"
)

// Job represents a billing job to be executed
type Job struct {
	ID             string
	SubscriptionID string
	Type           string
	Status         JobStatus
	ScheduledAt    time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	Attempts       int
	MaxAttempts    int
	LastError      string
	Payload        map[string]interface{}
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// JobStore defines the interface for job persistence
type JobStore interface {
	Create(job *Job) error
	Get(id string) (*Job, error)
	Update(job *Job) error
	ListPending(limit int) ([]*Job, error)
	ListDeadLetter() ([]*Job, error)
	AcquireLock(jobID string, workerID string, ttl time.Duration) (bool, error)
	ReleaseLock(jobID string, workerID string) error
}

