package metrics

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Pool-level Prometheus metrics.
// These are package-level vars so they are registered once at program start
// (promauto registers on the default registry automatically).
var (
	// dbPoolAcquiredConns is the number of connections currently checked out.
	dbPoolAcquiredConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_acquired_conns",
		Help: "Number of connections currently acquired (checked out) from the pool.",
	})

	// dbPoolIdleConns is the number of idle connections waiting in the pool.
	dbPoolIdleConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_idle_conns",
		Help: "Number of idle connections in the pool.",
	})

	// dbPoolTotalConns is the total number of connections managed by the pool
	// (acquired + idle + constructing).
	dbPoolTotalConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_total_conns",
		Help: "Total number of connections in the pool (acquired + idle + constructing).",
	})

	// dbPoolMaxConns is the configured maximum — kept as a gauge so dashboards
	// can compute the saturation ratio without hard-coding the limit.
	dbPoolMaxConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_max_conns",
		Help: "Configured maximum number of connections in the pool.",
	})

	// dbPoolConstructingConns is the number of connections currently being
	// established.  A sustained non-zero value under load indicates the pool
	// ceiling is too low.
	dbPoolConstructingConns = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "db_pool_constructing_conns",
		Help: "Number of connections currently being established.",
	})

	// dbPoolAcquireCount is the cumulative number of successful Acquire calls.
	// Use rate() in PromQL to get throughput.
	dbPoolAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "db_pool_acquire_count_total",
		Help: "Total number of successful connection acquisitions from the pool.",
	})

	// dbPoolCanceledAcquireCount is the number of Acquire calls that were
	// cancelled or timed out before a connection was available.  Any non-zero
	// value in production is a saturation signal.
	dbPoolCanceledAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "db_pool_canceled_acquire_total",
		Help: "Total number of connection acquisitions cancelled due to context cancellation or timeout.",
	})

	// dbPoolAcquireDuration is a histogram of the time callers waited before
	// receiving a connection.  Watch the p99 — it rises before errors appear.
	dbPoolAcquireDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "db_pool_acquire_duration_seconds",
		Help:    "Time spent waiting to acquire a connection from the pool.",
		Buckets: []float64{.0001, .0005, .001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
	})

	// dbPoolEmptyAcquireCount is the number of times Acquire had to wait
	// because the pool was empty (all connections in use).
	dbPoolEmptyAcquireCount = promauto.NewCounter(prometheus.CounterOpts{
		Name: "db_pool_empty_acquire_total",
		Help: "Total number of times Acquire had to wait because the pool was empty.",
	})
)

// poolStatSnapshot is a value-copy of pgxpool.Stat fields we care about.
// Using a snapshot avoids holding a reference to the pool inside the scraper.
type poolStatSnapshot struct {
	acquiredConns       int32
	idleConns           int32
	totalConns          int32
	maxConns            int32
	constructingConns   int32
	acquireCount        int64
	canceledAcquire     int64
	acquireDuration     time.Duration
	emptyAcquireCount   int64
}

func snapshotPool(pool *pgxpool.Pool) poolStatSnapshot {
	s := pool.Stat()
	return poolStatSnapshot{
		acquiredConns:     s.AcquiredConns(),
		idleConns:         s.IdleConns(),
		totalConns:        s.TotalConns(),
		maxConns:          s.MaxConns(),
		constructingConns: s.ConstructingConns(),
		acquireCount:      s.AcquireCount(),
		canceledAcquire:   s.CanceledAcquireCount(),
		acquireDuration:   s.AcquireDuration(),
		emptyAcquireCount: s.EmptyAcquireCount(),
	}
}

// poolScraper holds the previous snapshot so we can compute deltas for
// counters (pgxpool exposes cumulative values; Prometheus counters must only
// increase, so we add the delta each tick).
type poolScraper struct {
	pool     *pgxpool.Pool
	prev     poolStatSnapshot
	interval time.Duration
}

func newPoolScraper(pool *pgxpool.Pool, interval time.Duration) *poolScraper {
	return &poolScraper{pool: pool, interval: interval}
}

func (ps *poolScraper) scrape() {
	cur := snapshotPool(ps.pool)

	// Gauges — set to current value directly.
	dbPoolAcquiredConns.Set(float64(cur.acquiredConns))
	dbPoolIdleConns.Set(float64(cur.idleConns))
	dbPoolTotalConns.Set(float64(cur.totalConns))
	dbPoolMaxConns.Set(float64(cur.maxConns))
	dbPoolConstructingConns.Set(float64(cur.constructingConns))

	// Counters — add the delta since the last scrape.
	if delta := cur.acquireCount - ps.prev.acquireCount; delta > 0 {
		dbPoolAcquireCount.Add(float64(delta))
	}
	if delta := cur.canceledAcquire - ps.prev.canceledAcquire; delta > 0 {
		dbPoolCanceledAcquireCount.Add(float64(delta))
	}
	if delta := cur.emptyAcquireCount - ps.prev.emptyAcquireCount; delta > 0 {
		dbPoolEmptyAcquireCount.Add(float64(delta))
	}

	// Histogram — observe the cumulative acquire duration delta converted to
	// a per-acquire average for this interval.  This is an approximation but
	// gives a useful signal without instrumenting every Acquire call.
	if acquireDelta := cur.acquireCount - ps.prev.acquireCount; acquireDelta > 0 {
		durDelta := cur.acquireDuration - ps.prev.acquireDuration
		if durDelta > 0 {
			avgWait := durDelta.Seconds() / float64(acquireDelta)
			dbPoolAcquireDuration.Observe(avgWait)
		}
	}

	ps.prev = cur
}

// StartPoolScraper launches a background goroutine that scrapes pool statistics
// every interval and updates the Prometheus metrics.  It returns a stop
// function; call it during graceful shutdown.
//
// If interval is <= 0 it defaults to 15 seconds.
func StartPoolScraper(pool *pgxpool.Pool, interval time.Duration) (stop func()) {
	if interval <= 0 {
		interval = 15 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	scraper := newPoolScraper(pool, interval)

	// Emit an initial snapshot immediately so dashboards are populated before
	// the first tick fires.
	scraper.scrape()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				scraper.scrape()
			case <-ctx.Done():
				return
			}
		}
	}()

	return cancel
}
