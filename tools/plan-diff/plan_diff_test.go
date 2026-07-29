package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDetectRegressions(t *testing.T) {
	prev := []StatementSnapshot{{Query: "SELECT 1", MeanTime: 10, TotalTime: 100, Calls: 10, SharedBlksRead: 0, SharedBlksHit: 0, SharedBlksDirtied: 0}}
	curr := []StatementSnapshot{{Query: "SELECT 1", MeanTime: 25, TotalTime: 150, Calls: 10, SharedBlksRead: 0, SharedBlksHit: 0, SharedBlksDirtied: 0}}

	regs := detectRegressions(prev, curr)
	require.Len(t, regs, 1)
	require.Equal(t, "SELECT 1", regs[0].Query)
	require.Equal(t, "mean_time_doubled", regs[0].Reason)
}

func TestDetectRegressions_NewQuery(t *testing.T) {
	prev := []StatementSnapshot{}
	curr := []StatementSnapshot{{Query: "SELECT new", MeanTime: 4, TotalTime: 20, Calls: 1, SharedBlksRead: 0, SharedBlksHit: 0, SharedBlksDirtied: 0}}

	regs := detectRegressions(prev, curr)
	require.Len(t, regs, 1)
	require.Equal(t, "new_query", regs[0].Reason)
}

func TestDetectRegressions_ScanTypeChange(t *testing.T) {
	prev := []StatementSnapshot{{Query: "SELECT * FROM users", MeanTime: 3, TotalTime: 30, Calls: 2, SharedBlksRead: 0, SharedBlksHit: 0, SharedBlksDirtied: 0, ScanType: "IndexScan"}}
	curr := []StatementSnapshot{{Query: "SELECT * FROM users", MeanTime: 6, TotalTime: 40, Calls: 2, SharedBlksRead: 0, SharedBlksHit: 0, SharedBlksDirtied: 0, ScanType: "SeqScan"}}

	regs := detectRegressions(prev, curr)
	require.Len(t, regs, 1)
	require.Equal(t, "scan_type_regressed", regs[0].Reason)
}

func TestDetectRegressions_NoRegression(t *testing.T) {
	prev := []StatementSnapshot{{Query: "SELECT 1", MeanTime: 10, TotalTime: 100, Calls: 10, SharedBlksRead: 0, SharedBlksHit: 0, SharedBlksDirtied: 0, ScanType: "IndexScan"}}
	curr := []StatementSnapshot{{Query: "SELECT 1", MeanTime: 12, TotalTime: 110, Calls: 10, SharedBlksRead: 0, SharedBlksHit: 0, SharedBlksDirtied: 0, ScanType: "IndexScan"}}

	regs := detectRegressions(prev, curr)
	require.Empty(t, regs)
}
