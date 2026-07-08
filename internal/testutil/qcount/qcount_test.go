package qcount

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeCountsOnlyTrackedQueriesWithoutArguments(t *testing.T) {
	probe := &Probe{}
	trackedContext, request := probe.Track(context.Background())

	probe.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{
		SQL:  "SELECT ignored",
		Args: []any{"not-counted"},
	})
	probe.TraceQueryStart(trackedContext, nil, pgx.TraceQueryStartData{
		SQL:  " SELECT * FROM subscriptions WHERE customer = $1 ",
		Args: []any{"secret-customer-id"},
	})

	snapshot := request.Snapshot()
	require.Equal(t, 1, snapshot.Count)
	require.Equal(t, []string{"SELECT * FROM subscriptions WHERE customer = $1"}, snapshot.Queries)
	assert.NotContains(t, strings.Join(snapshot.Queries, "\n"), "secret-customer-id")
}

func TestProbeBoundsDiagnosticStorage(t *testing.T) {
	probe := &Probe{}
	ctx, request := probe.Track(context.Background())
	longSQL := strings.Repeat("x", maxSQLLength+100)

	for i := 0; i < maxRecordedQueries+10; i++ {
		probe.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: longSQL})
	}

	snapshot := request.Snapshot()
	assert.Equal(t, maxRecordedQueries+10, snapshot.Count)
	require.Len(t, snapshot.Queries, maxRecordedQueries)
	assert.LessOrEqual(t, len(snapshot.Queries[0]), maxSQLLength+len("…"))
}

func TestCheckResultSizeInvariantReportsExecutedSQL(t *testing.T) {
	baseline := Sample{
		Label:      "small",
		ResultSize: 1,
		Snapshot: Snapshot{
			Count:   2,
			Queries: []string{"SELECT subscriptions", "SELECT plans"},
		},
	}
	candidate := Sample{
		Label:      "large",
		ResultSize: 20,
		Snapshot: Snapshot{
			Count:   21,
			Queries: []string{"SELECT subscriptions", "SELECT plan WHERE id = $1"},
		},
	}

	err := CheckResultSizeInvariant(baseline, candidate, 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "possible N+1 query growth")
	assert.Contains(t, err.Error(), "SELECT plan WHERE id = $1")
	assert.Contains(t, err.Error(), "20 results with 21 queries")
}

func TestCheckResultSizeInvariantAllowsFixedFilterExpansion(t *testing.T) {
	unfiltered := Sample{
		Label:      "unfiltered",
		ResultSize: 1,
		Snapshot:   Snapshot{Count: 1, Queries: []string{"SELECT statements"}},
	}
	filtered := Sample{
		Label:      "filtered",
		ResultSize: 20,
		Snapshot: Snapshot{
			Count:   2,
			Queries: []string{"SELECT validate_filter", "SELECT statements"},
		},
	}

	require.NoError(t, CheckResultSizeInvariant(unfiltered, filtered, 1))
}

func TestCheckResultSizeInvariantRejectsInvalidComparison(t *testing.T) {
	sample := Sample{Label: "same", ResultSize: 5}

	assert.Error(t, CheckResultSizeInvariant(sample, sample, 0))
	assert.Error(t, CheckResultSizeInvariant(
		Sample{ResultSize: 1},
		Sample{ResultSize: 2},
		-1,
	))
}
