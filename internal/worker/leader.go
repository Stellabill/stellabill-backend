package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// LeaderLocker exposes PostgreSQL advisory-lock behavior for singleton worker jobs.
type LeaderLocker interface {
	AcquireLock(ctx context.Context, key int64) (bool, error)
	ReleaseLock(ctx context.Context, key int64) error
	Close() error
}

// postgresLeaderLocker holds a dedicated SQL connection so advisory locks can be
// released by the same session that acquired them.
type postgresLeaderLocker struct {
	conn *sql.Conn
}

func NewPostgresLeaderLocker(db *sql.DB) (LeaderLocker, error) {
	if db == nil {
		return nil, errors.New("database connection required for leader election")
	}

	conn, err := db.Conn(context.Background())
	if err != nil {
		return nil, fmt.Errorf("acquire leader-election connection: %w", err)
	}

	return &postgresLeaderLocker{conn: conn}, nil
}

func (l *postgresLeaderLocker) AcquireLock(ctx context.Context, key int64) (bool, error) {
	if l == nil || l.conn == nil {
		return false, errors.New("leader-election connection unavailable")
	}

	var acquired bool
	err := l.conn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", key).Scan(&acquired)
	if err != nil {
		return false, fmt.Errorf("acquire advisory lock: %w", err)
	}
	return acquired, nil
}

func (l *postgresLeaderLocker) ReleaseLock(ctx context.Context, key int64) error {
	if l == nil || l.conn == nil {
		return nil
	}

	var released bool
	err := l.conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", key).Scan(&released)
	if err != nil {
		return fmt.Errorf("release advisory lock: %w", err)
	}
	_ = released
	return nil
}

func (l *postgresLeaderLocker) Close() error {
	if l == nil || l.conn == nil {
		return nil
	}
	conn := l.conn
	l.conn = nil
	return conn.Close()
}

var (
	leaderStatusMetricsOnce sync.Once
	leaderStatusMetrics     *prometheus.GaugeVec
)

func initLeaderStatusMetrics() {
	leaderStatusMetricsOnce.Do(func() {
		leaderStatusMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "leader_status",
			Help: "Whether a singleton worker job currently holds the advisory lock.",
		}, []string{"job"})
	})
}

func setLeaderStatus(job string, value float64) {
	initLeaderStatusMetrics()
	leaderStatusMetrics.WithLabelValues(job).Set(value)
}

var errNotLeader = errors.New("not leader")

type leaderGuard struct {
	locker LeaderLocker
	job    string
	key    int64

	mu   sync.Mutex
	hold bool
}

func newLeaderGuard(locker LeaderLocker, job string, key int64) *leaderGuard {
	return &leaderGuard{locker: locker, job: job, key: key}
}

func (g *leaderGuard) Run(ctx context.Context, fn func(context.Context) error) error {
	if g == nil || g.locker == nil {
		return fn(ctx)
	}

	acquired, err := g.locker.AcquireLock(ctx, g.key)
	if err != nil {
		setLeaderStatus(g.job, 0)
		return fmt.Errorf("acquire leader lock for %s: %w", g.job, err)
	}
	if !acquired {
		setLeaderStatus(g.job, 0)
		return errNotLeader
	}

	g.mu.Lock()
	g.hold = true
	g.mu.Unlock()
	setLeaderStatus(g.job, 1)

	defer func() {
		g.mu.Lock()
		hold := g.hold
		g.hold = false
		g.mu.Unlock()
		if hold {
			setLeaderStatus(g.job, 0)
			_ = g.locker.ReleaseLock(context.Background(), g.key)
		}
	}()

	return fn(ctx)
}

func (g *leaderGuard) Release(ctx context.Context) {
	if g == nil || g.locker == nil {
		return
	}

	g.mu.Lock()
	if !g.hold {
		g.mu.Unlock()
		return
	}
	g.hold = false
	g.mu.Unlock()

	setLeaderStatus(g.job, 0)
	_ = g.locker.ReleaseLock(ctx, g.key)
}

func (g *leaderGuard) Close() error {
	if g == nil || g.locker == nil {
		return nil
	}
	return g.locker.Close()
}
