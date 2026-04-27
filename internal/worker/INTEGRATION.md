# Worker Integration Guide

## Quick Start

### 1. Add Worker to Server

Update `cmd/server/main.go`:

```go
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/config"
	"stellarbill-backend/internal/routes"
	"stellarbill-backend/internal/timeutil"
	"stellarbill-backend/internal/worker"
)

func main() {
	cfg := config.Load()
	if cfg.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Setup router
	router := gin.Default()
	routes.Register(router)

	// Setup billing worker
	store := worker.NewMemoryStore()
	executor := worker.NewBillingExecutor()
	workerCfg := worker.DefaultConfig()
	workerCfg.PollInterval = 5 * time.Second
	workerCfg.MaxAttempts = 3

	billingWorker := worker.NewWorker(store, executor, workerCfg)
	billingWorker.Start()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// Start server in goroutine
	addr := ":" + cfg.Port
	go func() {
		log.Printf("Stellarbill backend listening on %s", addr)
		if err := router.Run(addr); err != nil {
			log.Fatal(err)
		}
	}()

	// Wait for shutdown signal
	<-quit
	log.Println("Shutting down server...")

	// Stop worker gracefully
	if err := billingWorker.Stop(); err != nil {
		log.Printf("Worker shutdown error: %v", err)
	}

	log.Println("Server stopped")
}
```

### 2. Add Configuration

Update `internal/config/config.go`:

```go
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Env                string
	Port               string
	DBConn             string
	JWTSecret          string
	WorkerEnabled      bool
	WorkerPollInterval time.Duration
	WorkerMaxAttempts  int
}

func Load() Config {
	pollInterval, _ := strconv.Atoi(getEnv("WORKER_POLL_INTERVAL", "5"))
	maxAttempts, _ := strconv.Atoi(getEnv("WORKER_MAX_ATTEMPTS", "3"))

	return Config{
		Env:                getEnv("ENV", "development"),
		Port:               getEnv("PORT", "8080"),
		DBConn:             getEnv("DATABASE_URL", "postgres://localhost/stellarbill?sslmode=disable"),
		JWTSecret:          getEnv("JWT_SECRET", "change-me-in-production"),
		WorkerEnabled:      getEnv("WORKER_ENABLED", "true") == "true",
		WorkerPollInterval: time.Duration(pollInterval) * time.Second,
		WorkerMaxAttempts:  maxAttempts,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

### 3. Add Job Management API

Create `internal/handlers/jobs.go`:

```go
package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/worker"
)

var (
	jobStore  worker.JobStore
	scheduler *worker.Scheduler
)

func InitJobHandlers(store worker.JobStore) {
	jobStore = store
	scheduler = worker.NewScheduler(store)
}

// POST /api/jobs/charge
func ScheduleCharge(c *gin.Context) {
	var req struct {
		SubscriptionID string    `json:"subscription_id" binding:"required"`
		ScheduledAt    time.Time `json:"scheduled_at"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.ScheduledAt.IsZero() {
		req.ScheduledAt = timeutil.NowUTC()
	}

	job, err := scheduler.ScheduleCharge(req.SubscriptionID, req.ScheduledAt, 3)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to schedule job"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"job": job})
}

// GET /api/jobs/:id
func GetJob(c *gin.Context) {
	jobID := c.Param("id")

	job, err := jobStore.Get(jobID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Job not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"job": job})
}

// GET /api/jobs/dead-letter
func ListDeadLetterJobs(c *gin.Context) {
	jobs, err := jobStore.ListDeadLetter()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list jobs"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}
```

### 4. Register Job Routes

Update `internal/routes/routes.go`:

```go
package routes

import (
	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/handlers"
)

func Register(r *gin.Engine) {
	r.Use(corsMiddleware())

	api := r.Group("/api")
	{
		api.GET("/health", handlers.Health)
		api.GET("/subscriptions", handlers.ListSubscriptions)
		api.GET("/subscriptions/:id", handlers.GetSubscription)
		api.GET("/plans", handlers.ListPlans)

		// Job management endpoints
		api.POST("/jobs/charge", handlers.ScheduleCharge)
		api.GET("/jobs/:id", handlers.GetJob)
		api.GET("/jobs/dead-letter", handlers.ListDeadLetterJobs)
	}
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
```

## Environment Variables

Add to `.env`:

```bash
# Worker Configuration
WORKER_ENABLED=true
WORKER_POLL_INTERVAL=5
WORKER_MAX_ATTEMPTS=3
```

## Testing the Integration

### 1. Start the Server

```bash
go run ./cmd/server
```

### 2. Schedule a Billing Job

```bash
curl -X POST http://localhost:8080/api/jobs/charge \
  -H "Content-Type: application/json" \
  -d '{
    "subscription_id": "sub-123",
    "scheduled_at": "2024-03-23T10:00:00Z"
  }'
```

Response:
```json
{
  "job": {
    "id": "charge-1234567890",
    "subscription_id": "sub-123",
    "type": "charge",
    "status": "pending",
    "scheduled_at": "2024-03-23T10:00:00Z",
    "attempts": 0,
    "max_attempts": 3
  }
}
```

### 3. Check Job Status

```bash
curl http://localhost:8080/api/jobs/charge-1234567890
```

### 4. List Failed Jobs

```bash
curl http://localhost:8080/api/jobs/dead-letter
```

## Database Integration

### PostgreSQL Store Implementation

Create `internal/worker/store_postgres.go`:

```go
package worker

import (
	"database/sql"
	"encoding/json"
	"time"

	_ "github.com/lib/pq"
	"stellarbill-backend/internal/timeutil"
)

type PostgresStore struct {
	db *sql.DB
}

func NewPostgresStore(connString string) (*PostgresStore, error) {
	db, err := sql.Open("postgres", connString)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return &PostgresStore{db: db}, nil
}

func (s *PostgresStore) Create(job *Job) error {
	payload, _ := json.Marshal(job.Payload)
	nowUTC := timeutil.NowUTC()

	_, err := s.db.Exec(`
		INSERT INTO jobs (
			id, subscription_id, type, status, scheduled_at,
			max_attempts, attempts, payload, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, job.ID, job.SubscriptionID, job.Type, job.Status, job.ScheduledAt,
		job.MaxAttempts, job.Attempts, payload, nowUTC, nowUTC)

	return err
}

func (s *PostgresStore) Get(id string) (*Job, error) {
	var job Job
	var payload []byte

	err := s.db.QueryRow(`
		SELECT id, subscription_id, type, status, scheduled_at,
			   started_at, completed_at, attempts, max_attempts,
			   last_error, payload, created_at, updated_at
		FROM jobs WHERE id = $1
	`, id).Scan(
		&job.ID, &job.SubscriptionID, &job.Type, &job.Status, &job.ScheduledAt,
		&job.StartedAt, &job.CompletedAt, &job.Attempts, &job.MaxAttempts,
		&job.LastError, &payload, &job.CreatedAt, &job.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrJobNotFound
		}
		return nil, err
	}

	json.Unmarshal(payload, &job.Payload)
	return &job, nil
}

func (s *PostgresStore) Update(job *Job) error {
	payload, _ := json.Marshal(job.Payload)
	nowUTC := timeutil.NowUTC()

	result, err := s.db.Exec(`
		UPDATE jobs SET
			status = $1, started_at = $2, completed_at = $3,
			attempts = $4, last_error = $5, payload = $6,
			scheduled_at = $7, updated_at = $8
		WHERE id = $9
	`, job.Status, job.StartedAt, job.CompletedAt, job.Attempts,
		job.LastError, payload, job.ScheduledAt, nowUTC, job.ID)

	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return ErrJobNotFound
	}

	return nil
}

func (s *PostgresStore) ListPending(limit int) ([]*Job, error) {
	rows, err := s.db.Query(`
		SELECT id, subscription_id, type, status, scheduled_at,
			   started_at, completed_at, attempts, max_attempts,
			   last_error, payload, created_at, updated_at
		FROM jobs
		WHERE status = $1 AND scheduled_at <= $2
		ORDER BY scheduled_at ASC
		LIMIT $3
	`, JobStatusPending, timeutil.NowUTC(), limit)

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var job Job
		var payload []byte

		err := rows.Scan(
			&job.ID, &job.SubscriptionID, &job.Type, &job.Status, &job.ScheduledAt,
			&job.StartedAt, &job.CompletedAt, &job.Attempts, &job.MaxAttempts,
			&job.LastError, &payload, &job.CreatedAt, &job.UpdatedAt,
		)
		if err != nil {
			continue
		}

		json.Unmarshal(payload, &job.Payload)
		jobs = append(jobs, &job)
	}

	return jobs, nil
}

func (s *PostgresStore) ListDeadLetter() ([]*Job, error) {
	// Similar to ListPending but filter by JobStatusDeadLetter
	// ... implementation
	return nil, nil
}

func (s *PostgresStore) AcquireLock(jobID string, workerID string, ttl time.Duration) (bool, error) {
	result, err := s.db.Exec(`
		UPDATE jobs
		SET locked_by = $1, locked_until = $2
		WHERE id = $3 AND (locked_until IS NULL OR locked_until < NOW())
	`, workerID, timeutil.NowUTC().Add(ttl), jobID)

	if err != nil {
		return false, err
	}

	rows, _ := result.RowsAffected()
	return rows > 0, nil
}

func (s *PostgresStore) ReleaseLock(jobID string, workerID string) error {
	_, err := s.db.Exec(`
		UPDATE jobs
		SET locked_by = NULL, locked_until = NULL
		WHERE id = $1 AND locked_by = $2
	`, jobID, workerID)

	return err
}
```

### Database Migration

Create `migrations/001_create_jobs_table.sql`:

```sql
CREATE TABLE jobs (
    id VARCHAR(255) PRIMARY KEY,
    subscription_id VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,
    status VARCHAR(50) NOT NULL,
    scheduled_at TIMESTAMP NOT NULL,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    attempts INTEGER DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    last_error TEXT,
    payload JSONB,
    locked_by VARCHAR(255),
    locked_until TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    INDEX idx_status_scheduled (status, scheduled_at),
    INDEX idx_locked_until (locked_until)
);
```

## Monitoring

### Metrics Endpoint

Add to `internal/handlers/metrics.go`:

```go
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"stellarbill-backend/internal/worker"
)

var workerInstance *worker.Worker

func InitMetrics(w *worker.Worker) {
	workerInstance = w
}

func GetMetrics(c *gin.Context) {
	if workerInstance == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Worker not initialized"})
		return
	}

	metrics := workerInstance.GetMetrics()
	c.JSON(http.StatusOK, gin.H{
		"jobs_processed":    metrics.JobsProcessed,
		"jobs_succeeded":    metrics.JobsSucceeded,
		"jobs_failed":       metrics.JobsFailed,
		"jobs_dead_lettered": metrics.JobsDeadLettered,
		"last_poll_time":    metrics.LastPollTime,
	})
}
```

### Health Check

Update `internal/handlers/health.go`:

```go
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"service": "stellarbill-backend",
		"status":  "ok",
		"worker":  "running",
	})
}
```

## Deployment

### Docker

Create `Dockerfile`:

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server ./cmd/server

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/server .
EXPOSE 8080
CMD ["./server"]
```

### Docker Compose

Create `docker-compose.yml`:

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: stellarbill
      POSTGRES_USER: stellarbill
      POSTGRES_PASSWORD: password
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

  worker:
    build: .
    environment:
      DATABASE_URL: postgres://stellarbill:password@postgres:5432/stellarbill?sslmode=disable
      WORKER_ENABLED: "true"
      WORKER_POLL_INTERVAL: "5"
      WORKER_MAX_ATTEMPTS: "3"
    depends_on:
      - postgres
    ports:
      - "8080:8080"

volumes:
  postgres_data:
```

Run:
```bash
docker-compose up
```

## Next Steps

1. Implement PostgreSQL store
2. Add authentication to job endpoints
3. Set up monitoring and alerting
4. Configure production database
5. Add integration tests
6. Set up CI/CD pipeline
7. Configure backup and recovery
8. Implement job archival
