package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if len(os.Args) > 1 {
		fmt.Fprintf(os.Stderr, "usage: plan-diff\n")
		os.Exit(2)
	}
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}

	current, err := loadCurrentSnapshots(ctx, pool)
	if err != nil {
		return err
	}

	baseline, err := loadLatestBaseline(ctx, pool)
	if err != nil {
		return err
	}

	regressions := detectRegressions(baseline, current)
	if len(regressions) == 0 {
		fmt.Println("No query plan regressions detected.")
		return nil
	}

	fmt.Println("Detected query plan regressions:")
	for _, reg := range regressions {
		fmt.Printf("- %s [%s]: %s\n", reg.Query, reg.Reason, reg.Detail)
	}
	return fmt.Errorf("query plan regression detected")
}

func loadCurrentSnapshots(ctx context.Context, pool *pgxpool.Pool) ([]StatementSnapshot, error) {
	rows, err := pool.Query(ctx, `
		SELECT
			query,
			mean_exec_time AS mean_time,
			total_exec_time AS total_time,
			calls,
			shared_blks_read,
			shared_blks_hit,
			shared_blks_dirtied
		FROM pg_stat_statements
		ORDER BY mean_exec_time DESC
		LIMIT 200
	`)
	if err != nil {
		return nil, fmt.Errorf("query pg_stat_statements: %w", err)
	}
	defer rows.Close()

	var snapshots []StatementSnapshot
	for rows.Next() {
		var snap StatementSnapshot
		if err := rows.Scan(&snap.Query, &snap.MeanTime, &snap.TotalTime, &snap.Calls, &snap.SharedBlksRead, &snap.SharedBlksHit, &snap.SharedBlksDirtied); err != nil {
			return nil, fmt.Errorf("scan pg_stat_statements row: %w", err)
		}
		snapshots = append(snapshots, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pg_stat_statements rows: %w", err)
	}
	return snapshots, nil
}

func loadLatestBaseline(ctx context.Context, pool *pgxpool.Pool) ([]StatementSnapshot, error) {
	rows, err := pool.Query(ctx, `
		SELECT query_text, mean_time, total_time, calls, shared_blks_read, shared_blks_hit, shared_blks_dirtied, scan_type
		FROM plan_baselines
		WHERE captured_at = (
			SELECT MAX(captured_at)
			FROM plan_baselines
		)
		ORDER BY mean_time DESC
		LIMIT 200
	`)
	if err != nil {
		return nil, fmt.Errorf("query plan_baselines: %w", err)
	}
	defer rows.Close()

	var baseline []StatementSnapshot
	for rows.Next() {
		var snap StatementSnapshot
		if err := rows.Scan(&snap.Query, &snap.MeanTime, &snap.TotalTime, &snap.Calls, &snap.SharedBlksRead, &snap.SharedBlksHit, &snap.SharedBlksDirtied, &snap.ScanType); err != nil {
			return nil, fmt.Errorf("scan plan_baselines row: %w", err)
		}
		baseline = append(baseline, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate plan_baselines rows: %w", err)
	}
	if len(baseline) == 0 {
		return nil, fmt.Errorf("no plan baseline found; run scripts/collect_plan_baseline.sh first")
	}
	return baseline, nil
}
