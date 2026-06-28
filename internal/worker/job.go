package worker

import (
	"sort"
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

// Priority represents the urgency of a job. Lower numeric values are more
// urgent and are processed before higher values.
type Priority int

const (
	PriorityHigh   Priority = 10
	PriorityNormal Priority = 20
	PriorityLow    Priority = 30
)

// DefaultLaneWeights controls how often each lane is selected in weighted
// round-robin scheduling. Out of totalWeight picks, High is chosen 3 times,
// Normal 2 times, Low 1 time.
var DefaultLaneWeights = map[Priority]int{
	PriorityHigh:   3,
	PriorityNormal: 2,
	PriorityLow:    1,
}

// laneOrder is the priority order used for starvation-guard fallback.
var laneOrder = []Priority{PriorityHigh, PriorityNormal, PriorityLow}

// totalWeight returns the sum of all lane weights.
func totalWeight() int {
	n := 0
	for _, w := range DefaultLaneWeights {
		n += w
	}
	return n
}

// Job represents a billing job to be executed
type Job struct {
	ID             string
	SubscriptionID string
	Type           string
	Status         JobStatus
	Priority       Priority
	ScheduledAt    time.Time
	StartedAt      *time.Time
	CompletedAt    *time.Time
	Attempts       int
	MaxAttempts    int
	LastError      string
	Payload        map[string]interface{}
	CreatedAt      time.Time
	UpdatedAt      time.Time

	// ParentTraceID links a job to its originating HTTP request trace.
	// Empty if the job was triggered manually or by a scheduler (no HTTP origin).
	ParentTraceID string
}

// JobStore defines the interface for job persistence
type JobStore interface {
	Create(job *Job) error
	Get(id string) (*Job, error)
	Update(job *Job) error
	ListPending(limit int) ([]*Job, error)
	ListPendingByPriority(priority Priority, limit int) ([]*Job, error)
	ListDeadLetter() ([]*Job, error)
	AcquireLock(jobID string, workerID string, ttl time.Duration) (bool, error)
	ReleaseLock(jobID string, workerID string) error

	QueueDepth() int
	LaneDepth(priority Priority) int
	OldestPending() *Job
}

// SortJobs sorts jobs by priority (highest first), then by ScheduledAt.
func SortJobs(jobs []*Job) {
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].Priority != jobs[j].Priority {
			return jobs[i].Priority < jobs[j].Priority
		}
		return jobs[i].ScheduledAt.Before(jobs[j].ScheduledAt)
	})
}
