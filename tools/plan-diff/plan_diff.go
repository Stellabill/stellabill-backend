package main

import (
	"sort"
)

type StatementSnapshot struct {
	Query             string
	MeanTime          float64
	TotalTime         float64
	Calls             int64
	SharedBlksRead    int64
	SharedBlksHit     int64
	SharedBlksDirtied int64
	ScanType          string
}

type Regression struct {
	Query  string
	Reason string
	Detail string
}

func detectRegressions(previous, current []StatementSnapshot) []Regression {
	prevByQuery := make(map[string]StatementSnapshot, len(previous))
	for _, snap := range previous {
		prevByQuery[snap.Query] = snap
	}

	var regressions []Regression
	for _, snap := range current {
		prev, ok := prevByQuery[snap.Query]
		if !ok {
			regressions = append(regressions, Regression{Query: snap.Query, Reason: "new_query", Detail: "query appeared in current snapshot"})
			continue
		}
		if prev.ScanType != "" && snap.ScanType != "" && prev.ScanType == "IndexScan" && snap.ScanType == "SeqScan" {
			regressions = append(regressions, Regression{Query: snap.Query, Reason: "scan_type_regressed", Detail: "index scan regressed to seq scan"})
			continue
		}
		if prev.MeanTime > 0 && snap.MeanTime >= prev.MeanTime*2 {
			regressions = append(regressions, Regression{Query: snap.Query, Reason: "mean_time_doubled", Detail: "mean_time doubled or exceeded"})
		}
	}

	sort.Slice(regressions, func(i, j int) bool {
		return regressions[i].Query < regressions[j].Query
	})
	return regressions
}
