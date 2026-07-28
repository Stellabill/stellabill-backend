package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"sort"
	"stellarbill-backend/internal/security"
	"sync"
	"time"
)

// shardedDispatcher implements the Dispatcher interface with partition-aware
// processing. It uses PostgreSQL advisory locks to coordinate shard ownership
// across multiple instances, ensuring disjoint processing without lock
// contention. Ordering within each tenant is preserved because all events for
// a given tenant are routed to the same partition via consistent hashing.
type shardedDispatcher struct {
	repository Repository
	publisher  Publisher
	publisherMap map[string]Publisher
	config     DispatcherConfig

	db      *sql.DB // for advisory lock management
	lockConn *sql.Conn // dedicated connection holding advisory locks

	ownedShards     map[int]struct{}
	ownedShardsList []int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	running bool
	mu     sync.RWMutex

	publisherFailCount   map[string]int
	publisherNextAttempt map[string]time.Time
}

// NewShardedDispatcher creates a dispatcher that only processes events whose
// partition number is in config.OwnedShards. It uses PostgreSQL session-level
// advisory locks (via a dedicated connection) so that multiple instances can
// safely process disjoint partitions in parallel.
//
// db is used exclusively for advisory lock management and must be a *sql.DB
// (not a transaction).
func NewShardedDispatcher(repository Repository, publisher Publisher, db *sql.DB, config DispatcherConfig) (Dispatcher, error) {
	if config.ShardCount <= 0 {
		return nil, fmt.Errorf("shard count must be positive for sharded dispatcher")
	}
	if len(config.OwnedShards) == 0 {
		return nil, fmt.Errorf("must own at least one shard")
	}
	if db == nil {
		return nil, fmt.Errorf("database connection required for advisory locks")
	}

	ownedSet := make(map[int]struct{}, len(config.OwnedShards))
	for _, s := range config.OwnedShards {
		if s < 0 || s >= config.ShardCount {
			return nil, fmt.Errorf("owned shard %d out of range [0, %d)", s, config.ShardCount)
		}
		ownedSet[s] = struct{}{}
	}

	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = 30 * time.Second
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = time.Hour
	}
	if config.CompletedEventTTL <= 0 {
		config.CompletedEventTTL = 24 * time.Hour
	}

	return &shardedDispatcher{
		repository: repository,
		publisher:  publisher,
		config:     config,
		db:         db,
		ownedShards:    ownedSet,
		ownedShardsList: config.OwnedShards,
	}, nil
}

// Start starts the sharded dispatcher. It acquires advisory locks for owned
// shards, then starts per-publisher drain goroutines, a heartbeat loop, and a
// cleanup loop.
func (d *shardedDispatcher) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return nil
	}

	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.running = true

	// Acquire a dedicated connection for advisory locks.
	var err error
	d.lockConn, err = d.db.Conn(d.ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire advisory lock connection: %w", err)
	}

	// Acquire advisory locks for all owned shards.
	if err := d.acquireShardLocks(); err != nil {
		d.lockConn.Close()
		return err
	}

	// Ensure publisher progress table exists.
	if err := d.repository.EnsurePublisherProgressTable(); err != nil {
		d.releaseAllLocks()
		d.lockConn.Close()
		return err
	}

	// Build publisher map.
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

	// Start per-publisher drain goroutines.
	for name, pub := range d.publisherMap {
		d.wg.Add(1)
		go d.publisherDrain(name, pub)
	}

	// Start heartbeat loop to verify advisory lock health.
	d.wg.Add(1)
	go d.heartbeatLoop()

	// Start cleanup loop.
	d.wg.Add(1)
	go d.cleanupLoop()

	log.Printf("Sharded outbox dispatcher started (owned shards: %v)", d.ownedShardsList)
	return nil
}

// Stop stops the sharded dispatcher. It releases advisory locks and closes the
// dedicated connection.
func (d *shardedDispatcher) Stop() error {
	d.mu.Lock()
	if !d.running {
		d.mu.Unlock()
		return nil
	}

	d.cancel()
	d.running = false
	d.mu.Unlock()

	d.wg.Wait()

	d.mu.Lock()
	d.releaseAllLocks()
	if d.lockConn != nil {
		d.lockConn.Close()
		d.lockConn = nil
	}
	d.mu.Unlock()

	log.Printf("%s", security.MaskPII("Sharded outbox dispatcher stopped"))
	return nil
}

// IsRunning returns whether the dispatcher is running.
func (d *shardedDispatcher) IsRunning() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.running
}

// acquireShardLocks tries to acquire PostgreSQL advisory locks for all owned
// shards using the dedicated connection. If a lock cannot be acquired (held by
// another instance), the shard is removed from the owned set.
func (d *shardedDispatcher) acquireShardLocks() error {
	for _, shard := range d.ownedShardsList {
		var acquired bool
		err := d.lockConn.QueryRowContext(d.ctx, "SELECT pg_try_advisory_lock($1)", shard).Scan(&acquired)
		if err != nil {
			return fmt.Errorf("failed to acquire advisory lock for shard %d: %w", shard, err)
		}
		if !acquired {
			log.Printf("Could not acquire advisory lock for shard %d (held by another instance)", shard)
			delete(d.ownedShards, shard)
			continue
		}
		log.Printf("Acquired advisory lock for shard %d", shard)
	}

	// Rebuild the owned shards list after removing unacquired shards.
	d.ownedShardsList = d.ownedShardsList[:0]
	for s := range d.ownedShards {
		d.ownedShardsList = append(d.ownedShardsList, s)
	}
	sort.Ints(d.ownedShardsList)

	if len(d.ownedShardsList) == 0 {
		return fmt.Errorf("no shards acquired; cannot start dispatcher")
	}

	return nil
}

// releaseAllLocks releases all held advisory locks.
func (d *shardedDispatcher) releaseAllLocks() {
	if d.lockConn == nil {
		return
	}
	for _, shard := range d.ownedShardsList {
		var released bool
		err := d.lockConn.QueryRowContext(context.Background(), "SELECT pg_advisory_unlock($1)", shard).Scan(&released)
		if err != nil {
			log.Printf("Error releasing advisory lock for shard %d: %v", shard, err)
		} else if released {
			log.Printf("Released advisory lock for shard %d", shard)
		}
	}
}

// heartbeatLoop periodically verifies that the advisory lock connection is
// alive. If the connection dies, owned shards are released to prevent silent
// stalls.
func (d *shardedDispatcher) heartbeatLoop() {
	defer d.wg.Done()

	ticker := time.NewTicker(d.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.verifyLockHealth()
		}
	}
}

func (d *shardedDispatcher) verifyLockHealth() {
	if d.lockConn == nil {
		return
	}
	if err := d.lockConn.PingContext(d.ctx); err != nil {
		log.Printf("Advisory lock connection lost: %v", err)
		d.mu.Lock()
		d.ownedShardsList = d.ownedShardsList[:0]
		d.ownedShards = make(map[int]struct{})
		d.mu.Unlock()
	}
}

// cleanupLoop handles cleanup of completed events.
func (d *shardedDispatcher) cleanupLoop() {
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

// publisherDrain processes events for a single publisher using its own cursor,
// filtering to only events in shards owned by this instance.
func (d *shardedDispatcher) publisherDrain(name string, pub Publisher) {
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

func (d *shardedDispatcher) drainOnceForPublisher(name string, pub Publisher) {
	// Respect backoff for this publisher.
	d.mu.RLock()
	next := d.publisherNextAttempt[name]
	ownedShards := d.currentShards()
	d.mu.RUnlock()

	if !next.IsZero() && time.Now().Before(next) {
		return
	}

	if len(ownedShards) == 0 {
		return
	}

	events, err := d.fetchEvents(ownedShards)
	if err != nil {
		log.Printf("Failed to get pending events for shards %v: %v", ownedShards, err)
		return
	}

	_ = name // used below in publish loop

	for _, event := range events {
		// Publish with timeout.
		ctx, cancel := context.WithTimeout(d.ctx, d.config.ProcessingTimeout)
		errCh := make(chan error, 1)
		go func(ev *Event) { errCh <- pub.Publish(ctx, ev) }(event)

		select {
		case err := <-errCh:
			cancel()
			if err != nil {
				log.Printf("Publisher %s failed for event %s: %v", name, event.ID, err)

				if IsPermanentPublishError(err) {
					errorMsg := err.Error()
					_ = d.repository.UpdateStatus(event.ID, StatusFailed, &errorMsg)
					continue
				}

				d.mu.Lock()
				d.publisherFailCount[name]++
				failCount := d.publisherFailCount[name]
				d.mu.Unlock()

				if failCount >= d.config.MaxRetries {
					errorMsg := err.Error()
					_ = d.repository.UpdateStatus(event.ID, StatusFailed, &errorMsg)
					d.mu.Lock()
					d.publisherFailCount[name] = 0
					d.publisherNextAttempt[name] = time.Time{}
					d.mu.Unlock()
					continue
				}

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

			// Success: reset failure state and acknowledge.
			d.mu.Lock()
			d.publisherFailCount[name] = 0
			d.publisherNextAttempt[name] = time.Time{}
			d.mu.Unlock()

			if err := d.repository.MarkPublished(name, event, d.publisherNames()); err != nil {
				log.Printf("Failed to mark event %s published for %s: %v", event.ID, name, err)
				continue
			}

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

// fetchEvents returns pending events for the given shards. If the repository
// supports ShardedRepository, it uses the efficient partition-filtered query.
// Otherwise it falls back to fetching all pending events and filtering.
func (d *shardedDispatcher) fetchEvents(shards []int) ([]*Event, error) {
	if sr, ok := d.repository.(ShardedRepository); ok {
		return sr.GetPendingEventsForShards(shards, d.config.BatchSize)
	}

	// Fallback: fetch more than batch size and filter in-memory.
	events, err := d.repository.GetPendingEvents(d.config.BatchSize * len(shards))
	if err != nil {
		return nil, err
	}

	shardSet := make(map[int]struct{}, len(shards))
	for _, s := range shards {
		shardSet[s] = struct{}{}
	}

	var filtered []*Event
	for _, e := range events {
		if _, ok := shardSet[e.Partition]; ok {
			filtered = append(filtered, e)
		}
		if len(filtered) >= d.config.BatchSize {
			break
		}
	}
	return filtered, nil
}

func (d *shardedDispatcher) publisherNames() []string {
	names := make([]string, 0, len(d.publisherMap))
	for name := range d.publisherMap {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// currentShards returns a snapshot of the currently owned shards.
func (d *shardedDispatcher) currentShards() []int {
	out := make([]int, len(d.ownedShardsList))
	copy(out, d.ownedShardsList)
	return out
}

// cleanupCompletedEvents removes old completed events.
func (d *shardedDispatcher) cleanupCompletedEvents() {
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
