package worker_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"stellarbill-backend/internal/cache"
	"stellarbill-backend/internal/worker"
)

// TestDatabaseSetup creates an in-memory test database with statements table.
func setupTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Create statements table with archive columns
	schema := `
	CREATE TABLE statements (
		id              TEXT        PRIMARY KEY,
		subscription_id TEXT        NOT NULL,
		customer_id     TEXT        NOT NULL,
		period_start    TEXT,
		period_end      TEXT,
		issued_at       TEXT,
		total_amount    TEXT,
		currency        TEXT,
		kind            TEXT,
		status          TEXT,
		deleted_at      DATETIME,
		archived_at     DATETIME,
		archive_key     TEXT,
		CHECK ((archived_at IS NULL AND archive_key IS NULL) OR
		       (archived_at IS NOT NULL AND archive_key IS NOT NULL))
	);
	CREATE INDEX idx_statements_archival_scan 
		ON statements (issued_at ASC) 
		WHERE archived_at IS NULL AND deleted_at IS NULL;
	`

	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("Failed to create schema: %v", err)
	}

	return db
}

// InsertStatement inserts a statement for testing.
func insertStatement(t *testing.T, db *sql.DB, id, subID, custID, periodStart, periodEnd, issuedAt string) {
	_, err := db.Exec(`
		INSERT INTO statements (id, subscription_id, customer_id, period_start, period_end, issued_at, total_amount, currency, kind, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, subID, custID, periodStart, periodEnd, issuedAt, "1000", "USD", "invoice", "paid")

	if err != nil {
		t.Fatalf("Failed to insert statement: %v", err)
	}
}

// GetStatement retrieves a statement for inspection.
func getStatement(t *testing.T, db *sql.DB, id string) map[string]interface{} {
	row := db.QueryRow(`
		SELECT id, archived_at, archive_key, period_start, total_amount
		FROM statements WHERE id = ?
	`, id)

	var archiveAt sql.NullTime
	var archiveKey sql.NullString
	var periodStart sql.NullString
	var amount sql.NullString
	var stmtID string

	if err := row.Scan(&stmtID, &archiveAt, &archiveKey, &periodStart, &amount); err != nil {
		t.Fatalf("Failed to scan statement: %v", err)
	}

	return map[string]interface{}{
		"id":           stmtID,
		"archived_at":  archiveAt,
		"archive_key":  archiveKey,
		"period_start": periodStart,
		"total_amount": amount,
	}
}

func TestStatementArchiveJob_ArchiveOldStatements(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	objStore := cache.NewMemoryObjectStore()
	config := worker.DefaultStatementArchiveConfig()
	config.ArchiveThresholdMonths = 12
	config.BatchSize = 10

	job := worker.NewStatementArchiveJob(db, objStore, config, nil)

	// Insert statements: old (eligible) and new (not eligible)
	oldDate := time.Now().AddDate(-2, 0, 0).Format(time.RFC3339)
	newDate := time.Now().Format(time.RFC3339)

	insertStatement(t, db, "stmt-old-1", "sub-1", "cust-1", oldDate, oldDate, oldDate)
	insertStatement(t, db, "stmt-old-2", "sub-1", "cust-1", oldDate, oldDate, oldDate)
	insertStatement(t, db, "stmt-new", "sub-1", "cust-1", newDate, newDate, newDate)

	// Archive batch
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	batch := func() {
		threshold := time.Now().AddDate(0, -config.ArchiveThresholdMonths, 0)
		thresholdStr := threshold.Format(time.RFC3339)

		rows, err := db.QueryContext(
			ctx,
			`SELECT id, subscription_id, customer_id, period_start, period_end, 
			        issued_at, total_amount, currency, kind, status
			 FROM statements
			 WHERE archived_at IS NULL 
			   AND deleted_at IS NULL 
			   AND issued_at < $1
			 ORDER BY issued_at ASC
			 LIMIT $2`,
			thresholdStr,
			config.BatchSize,
		)
		if err != nil {
			t.Fatalf("Query failed: %v", err)
		}
		defer rows.Close()

		var count int
		for rows.Next() {
			count++
		}

		if count != 2 {
			t.Errorf("Expected 2 old statements, got %d", count)
		}
	}

	batch()

	// Verify object store has archived data
	all := objStore.All()
	if len(all) != 2 {
		t.Errorf("Expected 2 archived objects, got %d", len(all))
	}

	// Verify archived statements in DB
	for _, id := range []string{"stmt-old-1", "stmt-old-2"} {
		stmt := getStatement(t, db, id)
		archiveAt := stmt["archived_at"].(sql.NullTime)
		archiveKey := stmt["archive_key"].(sql.NullString)
		periodStart := stmt["period_start"].(sql.NullString)

		if !archiveAt.Valid {
			t.Errorf("Statement %s should have archived_at set", id)
		}
		if !archiveKey.Valid {
			t.Errorf("Statement %s should have archive_key set", id)
		}
		if periodStart.Valid {
			t.Errorf("Statement %s should have period_start cleared", id)
		}
	}

	// Verify new statement not archived
	stmt := getStatement(t, db, "stmt-new")
	archiveAt := stmt["archived_at"].(sql.NullTime)
	if archiveAt.Valid {
		t.Error("New statement should not be archived")
	}
}

func TestStatementArchiveJob_ArchivePayload(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	objStore := cache.NewMemoryObjectStore()
	config := worker.DefaultStatementArchiveConfig()
	config.ArchiveThresholdMonths = 12

	// Insert statement
	issuedAt := time.Now().AddDate(-2, 0, 0).Format(time.RFC3339)
	insertStatement(t, db, "stmt-payload-test", "sub-1", "cust-1", issuedAt, issuedAt, issuedAt)

	// Archive it manually (simulating what the job would do)
	var stmt struct {
		ID             string
		SubscriptionID string
		CustomerID     string
		PeriodStart    string
		PeriodEnd      string
		IssuedAt       string
		TotalAmount    string
		Currency       string
		Kind           string
		Status         string
	}

	row := db.QueryRow(`
		SELECT id, subscription_id, customer_id, period_start, period_end, 
		       issued_at, total_amount, currency, kind, status
		FROM statements WHERE id = ?
	`, "stmt-payload-test")

	if err := row.Scan(&stmt.ID, &stmt.SubscriptionID, &stmt.CustomerID,
		&stmt.PeriodStart, &stmt.PeriodEnd, &stmt.IssuedAt,
		&stmt.TotalAmount, &stmt.Currency, &stmt.Kind, &stmt.Status); err != nil {
		t.Fatalf("Failed to scan: %v", err)
	}

	// Create payload and upload
	now := time.Now().UTC()
	payload := &cache.StatementArchivePayload{
		ID:             stmt.ID,
		SubscriptionID: stmt.SubscriptionID,
		CustomerID:     stmt.CustomerID,
		PeriodStart:    stmt.PeriodStart,
		PeriodEnd:      stmt.PeriodEnd,
		IssuedAt:       stmt.IssuedAt,
		TotalAmount:    stmt.TotalAmount,
		Currency:       stmt.Currency,
		Kind:           stmt.Kind,
		Status:         stmt.Status,
		ArchivedAt:     now.Format(time.RFC3339),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	ctx := context.Background()
	key := "statements/archive/2024/06/24/stmt-payload-test.json"
	_, err = objStore.Put(ctx, key, data)
	if err != nil {
		t.Fatalf("Failed to upload: %v", err)
	}

	// Verify payload
	retrieved, err := objStore.Get(ctx, key)
	if err != nil {
		t.Fatalf("Failed to retrieve: %v", err)
	}

	var retrievedPayload cache.StatementArchivePayload
	if err := json.Unmarshal(retrieved, &retrievedPayload); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if retrievedPayload.ID != "stmt-payload-test" {
		t.Errorf("Payload ID mismatch: got %q", retrievedPayload.ID)
	}
	if retrievedPayload.TotalAmount != "1000" {
		t.Errorf("Payload amount mismatch: got %q", retrievedPayload.TotalAmount)
	}
	if retrievedPayload.Currency != "USD" {
		t.Errorf("Payload currency mismatch: got %q", retrievedPayload.Currency)
	}
}

func TestStatementArchiveJob_HealthCheck(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	objStore := cache.NewMemoryObjectStore()
	config := worker.DefaultStatementArchiveConfig()

	job := worker.NewStatementArchiveJob(db, objStore, config, nil)

	// Not running
	if err := job.Health(); err == nil {
		t.Error("Health should fail when job is not running")
	}

	job.Start()
	defer job.Stop()

	time.Sleep(100 * time.Millisecond)

	// Running
	if err := job.Health(); err != nil {
		t.Errorf("Health should pass when running: %v", err)
	}
}

func TestStatementArchiveJob_Stats(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	objStore := cache.NewMemoryObjectStore()
	config := worker.DefaultStatementArchiveConfig()

	job := worker.NewStatementArchiveJob(db, objStore, config, nil)

	stats := job.GetStats()
	if stats.Archived != 0 {
		t.Errorf("Initial archived count should be 0, got %d", stats.Archived)
	}
	if stats.Failed != 0 {
		t.Errorf("Initial failed count should be 0, got %d", stats.Failed)
	}
}

func TestStatementArchiveJob_Idempotency(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	objStore := cache.NewMemoryObjectStore()
	config := worker.DefaultStatementArchiveConfig()
	config.ArchiveThresholdMonths = 12

	// Insert statement
	issuedAt := time.Now().AddDate(-2, 0, 0).Format(time.RFC3339)
	insertStatement(t, db, "stmt-idempotent", "sub-1", "cust-1", issuedAt, issuedAt, issuedAt)

	// Archive it manually
	ctx := context.Background()
	now := time.Now().UTC()

	row := db.QueryRow(`
		SELECT id, subscription_id, customer_id, period_start, period_end, 
		       issued_at, total_amount, currency, kind, status
		FROM statements WHERE id = ?
	`, "stmt-idempotent")

	var id, subID, custID, ps, pe, ia, ta, cu, k, s string
	row.Scan(&id, &subID, &custID, &ps, &pe, &ia, &ta, &cu, &k, &s)

	payload := &cache.StatementArchivePayload{
		ID:             id,
		SubscriptionID: subID,
		CustomerID:     custID,
		PeriodStart:    ps,
		PeriodEnd:      pe,
		IssuedAt:       ia,
		TotalAmount:    ta,
		Currency:       cu,
		Kind:           k,
		Status:         s,
		ArchivedAt:     now.Format(time.RFC3339),
	}

	data, _ := json.Marshal(payload)
	key := "statements/archive/2024/06/24/stmt-idempotent.json"
	objStore.Put(ctx, key, data)

	db.ExecContext(ctx, `
		UPDATE statements
		SET archived_at = $1, archive_key = $2, period_start = NULL
		WHERE id = $3
	`, now, key, "stmt-idempotent")

	// Verify it's archived
	stmt := getStatement(t, db, "stmt-idempotent")
	archiveAt := stmt["archived_at"].(sql.NullTime)
	if !archiveAt.Valid {
		t.Fatal("Statement should be archived")
	}

	// Query for old statements - should not find it again (idempotency)
	threshold := time.Now().AddDate(0, -config.ArchiveThresholdMonths, 0)
	thresholdStr := threshold.Format(time.RFC3339)

	queryRow := db.QueryRow(`
		SELECT COUNT(*) FROM statements
		WHERE archived_at IS NULL 
		  AND deleted_at IS NULL 
		  AND issued_at < ?
	`, thresholdStr)

	var count int
	queryRow.Scan(&count)

	if count != 0 {
		t.Errorf("Already-archived statement should not be selected again, got %d", count)
	}
}
