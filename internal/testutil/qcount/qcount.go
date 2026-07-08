// Package qcount provides a test-only pgx query tracer for detecting query
// counts that grow with a handler's result size.
package qcount

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maxRecordedQueries = 256
	maxSQLLength       = 4096
)

type requestKey struct{}

// Probe implements pgx.QueryTracer. Attach one Probe to a test pool and use
// Track to mark only the request whose queries should be counted.
type Probe struct{}

// Request contains the queries executed for one tracked request.
type Request struct {
	mu      sync.Mutex
	count   int
	queries []string
}

// Snapshot is an immutable view of a tracked request.
type Snapshot struct {
	Count   int
	Queries []string
}

// Sample associates a query snapshot with the number of results returned by
// the request. Label is included in assertion failures.
type Sample struct {
	Label      string
	ResultSize int
	Snapshot   Snapshot
}

// NewPool constructs a pgx pool with a query-counting tracer. The returned
// probe ignores all queries unless their context was produced by Probe.Track.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, *Probe, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("parse pgx pool configuration: %w", err)
	}

	probe := &Probe{}
	cfg.ConnConfig.Tracer = probe
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	cfg.MaxConns = 5
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create traced pgx pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("ping traced pgx pool: %w", err)
	}
	return pool, probe, nil
}

// Track returns a derived context and an initially empty per-request counter.
// Bound argument values are intentionally not recorded.
func (p *Probe) Track(ctx context.Context) (context.Context, *Request) {
	request := &Request{}
	return context.WithValue(ctx, requestKey{}, request), request
}

// TraceQueryStart implements pgx.QueryTracer.
func (p *Probe) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	request, ok := ctx.Value(requestKey{}).(*Request)
	if !ok || request == nil {
		return ctx
	}

	sql := strings.TrimSpace(data.SQL)
	if len(sql) > maxSQLLength {
		sql = sql[:maxSQLLength] + "…"
	}

	request.mu.Lock()
	request.count++
	if len(request.queries) < maxRecordedQueries {
		request.queries = append(request.queries, sql)
	}
	request.mu.Unlock()

	return ctx
}

// TraceQueryEnd implements pgx.QueryTracer. Counting at query start ensures
// failed queries remain visible in the diagnostic.
func (p *Probe) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

// Snapshot returns a concurrency-safe copy of the request count and SQL.
func (r *Request) Snapshot() Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()

	queries := make([]string, len(r.queries))
	copy(queries, r.queries)
	return Snapshot{Count: r.count, Queries: queries}
}

// NewSample creates a result-size sample from a completed tracked request.
func NewSample(label string, resultSize int, request *Request) Sample {
	return Sample{
		Label:      label,
		ResultSize: resultSize,
		Snapshot:   request.Snapshot(),
	}
}

// CheckResultSizeInvariant fails when candidate performs more SQL statements
// than baseline plus maxExtraQueries. A small allowance supports legitimate,
// fixed-cost filter expansion without permitting per-result query growth.
func CheckResultSizeInvariant(baseline, candidate Sample, maxExtraQueries int) error {
	if baseline.ResultSize < 0 || candidate.ResultSize < 0 {
		return fmt.Errorf("qcount: result sizes must not be negative")
	}
	if candidate.ResultSize <= baseline.ResultSize {
		return fmt.Errorf(
			"qcount: candidate result size (%d) must exceed baseline result size (%d)",
			candidate.ResultSize,
			baseline.ResultSize,
		)
	}
	if maxExtraQueries < 0 {
		return fmt.Errorf("qcount: maxExtraQueries must not be negative")
	}

	limit := baseline.Snapshot.Count + maxExtraQueries
	if candidate.Snapshot.Count <= limit {
		return nil
	}

	return fmt.Errorf(
		"possible N+1 query growth: %s returned %d results with %d queries; "+
			"%s returned %d results with %d queries; allowed at most %d candidate queries\n"+
			"baseline SQL:\n%s\ncandidate SQL:\n%s",
		candidate.Label,
		candidate.ResultSize,
		candidate.Snapshot.Count,
		baseline.Label,
		baseline.ResultSize,
		baseline.Snapshot.Count,
		limit,
		formatQueries(baseline.Snapshot),
		formatQueries(candidate.Snapshot),
	)
}

func formatQueries(snapshot Snapshot) string {
	if len(snapshot.Queries) == 0 {
		return "  (none)"
	}

	var output strings.Builder
	for i, sql := range snapshot.Queries {
		fmt.Fprintf(&output, "  %d. %s\n", i+1, sql)
	}
	if unrecorded := snapshot.Count - len(snapshot.Queries); unrecorded > 0 {
		fmt.Fprintf(&output, "  … %d additional queries omitted\n", unrecorded)
	}
	return strings.TrimSuffix(output.String(), "\n")
}
