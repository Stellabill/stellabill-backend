# Statement Cold Archive Architecture

## Overview

The statement cold archive system enables efficient storage of old billing statements (>24 months) in cost-effective cold storage (e.g., S3, GCS) while maintaining transparent read access through automatic rehydration on demand.

### Problem Statement

Hot storage (Postgres) is expensive per gigabyte and optimized for frequent access. Billing statements older than 24 months are rarely accessed (<<1% of reads) but still consume disk space and slow down hot storage performance. This design moves aged statements to cold storage while keeping them transparently accessible to clients.

## Architecture

### Data Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                    Statement Archival Pipeline                  │
└─────────────────────────────────────────────────────────────────┘

1. Initial Creation (client request)
   ┌──────────┐
   │  Client  │ POST /statements
   └──────────┘
        │
        ↓
   ┌─────────────────────────────────┐
   │ StatementService.Create()       │
   │ - Store in Postgres (hot)       │
   │ - Full data retention           │
   └─────────────────────────────────┘
        │
        ↓
   ┌──────────────────────┐
   │ Postgres (hot)       │
   │ - All fields present │
   │ - archived_at: NULL  │
   │ - archive_key: NULL  │
   └──────────────────────┘

2. Archival Job (24h interval)
   ┌───────────────────────────────────────┐
   │ StatementArchiveJob.archiveLoop()     │
   │ - Runs every 24 hours                 │
   │ - Scans for statements > 24 months    │
   │ - With archived_at IS NULL filter     │
   └───────────────────────────────────────┘
        │
        ├─ Batch query: issued_at < (now - 24 months)
        │  WHERE archived_at IS NULL AND deleted_at IS NULL
        │
        ↓ for each statement
   ┌─────────────────────────────────┐
   │ Serialize to JSON               │
   │ - Preserve all original fields  │
   │ - Include ArchivedAt timestamp  │
   └─────────────────────────────────┘
        │
        ↓
   ┌─────────────────────────────────────────────┐
   │ Upload to Cold Storage (S3)                 │
   │ - Key: statements/archive/YYYY/MM/DD/ID.json│
   │ - Content: Full archived payload            │
   │ - Version: 1                                 │
   └─────────────────────────────────────────────┘
        │
        ↓
   ┌────────────────────────────────────────┐
   │ Update Postgres Row (transactional)    │
   │ UPDATE statements                      │
   │ SET archived_at = NOW(),               │
   │     archive_key = 's3://...',          │
   │     period_start = NULL,               │
   │     period_end = NULL,                 │
   │     issued_at = NULL,                  │
   │     total_amount = NULL,               │
   │     currency = NULL,                   │
   │     kind = NULL,                       │
   │     status = NULL                      │
   │ WHERE id = 'stmt-id'                   │
   └────────────────────────────────────────┘

3. Read Access (client request for archived statement)
   ┌──────────┐
   │  Client  │ GET /statements/{id}
   └──────────┘
        │
        ↓
   ┌──────────────────────────────────┐
   │ StatementService.GetDetail()     │
   │ - RBAC check (auth, ownership)   │
   └──────────────────────────────────┘
        │
        ↓
   ┌────────────────────────────────┐
   │ Query Postgres                 │
   │ SELECT * FROM statements       │
   │ WHERE id = 'stmt-id'           │
   └────────────────────────────────┘
        │
        ├─ IF archived_at IS NULL
        │  └─ Return active statement (fast path, <1ms)
        │
        └─ ELSE IF archived_at IS NOT NULL
           ├─ archive_key = 's3://...'
           │
           ↓
           ┌─────────────────────────────────────────┐
           │ Retrieve from Cold Storage (S3)         │
           │ - ObjectStore.Get(archive_key)          │
           │ - Latency: 100-500ms (typical)          │
           │ - Retry with exponential backoff        │
           └─────────────────────────────────────────┘
           │
           ↓
           ┌──────────────────────────────────────┐
           │ Parse JSON Payload                   │
           │ - Unmarshal into StatementArchivePayload
           │ - Reconstruct full StatementRow      │
           └──────────────────────────────────────┘
           │
           ↓
           ┌──────────────────────────────────────────┐
           │ Return to Client                         │
           │ - Full statement data                    │
           │ - Warning: "rehydrated from cold storage"│
           │ - Note latency contract to caller       │
           └──────────────────────────────────────────┘
           │
           └─ [ASYNC/BEST-EFFORT] Update Postgres cache
              UpdateArchivedData() - populate fields for next read
              (Failure ignored; optimization only)
```

### Database Schema

#### New Columns (Migration 0010)

```sql
ALTER TABLE statements ADD COLUMN archived_at TIMESTAMPTZ;
ALTER TABLE statements ADD COLUMN archive_key TEXT;

-- Consistency constraint: both NULL or both set
ALTER TABLE statements ADD CONSTRAINT check_archive_consistency
  CHECK ((archived_at IS NULL AND archive_key IS NULL) OR
         (archived_at IS NOT NULL AND archive_key IS NOT NULL));

-- Archival scan index
CREATE INDEX idx_statements_archival_scan
  ON statements (issued_at ASC)
  WHERE archived_at IS NULL AND deleted_at IS NULL;

-- Active statement lookup
CREATE INDEX idx_statements_active_id
  ON statements (id)
  WHERE archived_at IS NULL AND deleted_at IS NULL;
```

#### Active vs. Archived Row States

| State        | archived_at | archive_key | Data Fields    | Usage                               |
| ------------ | ----------- | ----------- | -------------- | ----------------------------------- |
| **Active**   | NULL        | NULL        | Present        | Hot storage; frequently accessed    |
| **Archived** | Timestamp   | S3 path     | NULL           | Cold storage stub; rarely accessed  |
| **Deleted**  | -           | -           | deleted_at set | Soft-deleted; excluded from queries |

### Components

#### 1. Object Storage Interface (`internal/cache/object_store.go`)

Abstracts over S3, GCS, or in-memory storage:

```go
type ObjectStore interface {
    Put(ctx context.Context, key string, data []byte) (string, error)
    Get(ctx context.Context, key string) ([]byte, error)
    Delete(ctx context.Context, key string) error
}
```

**Implementations:**

- `MemoryObjectStore`: For testing, development, and demonstrations
- (Production: Implement S3 adapter with `aws-sdk-go-v2`)

#### 2. Archive Job (`internal/worker/statement_archive_job.go`)

Runs on a 24-hour schedule to archive old statements:

```go
type StatementArchiveJob struct {
    db *sql.DB
    objStore cache.ObjectStore
    config StatementArchiveConfig
    // ...
}

// Runs periodically, scans for old statements, uploads to cold storage
func (j *StatementArchiveJob) archiveLoop()
```

**Configuration:**

- `ArchiveThresholdMonths`: 24 (default)
- `BatchSize`: 100 (tunable for DB load)
- `PollInterval`: 24h
- `ArchiveTimeout`: 5m per batch

**Guarantees:**

- Idempotent: archived statements are skipped (archived_at IS NOT NULL check)
- Transactional: S3 upload + DB update are consistent
- Rolled back on failure: Delete from S3 if DB update fails

#### 3. Statement Service Rehydration (`internal/service/statement_service.go`)

Enhanced `GetDetail()` method handles transparent rehydration:

```go
type statementService struct {
    subRepo  repository.SubscriptionRepository
    stmtRepo repository.StatementRepository
    objStore cache.ObjectStore  // optional
}

func (s *statementService) GetDetail(...) (*StatementDetail, []string, error) {
    // ... RBAC checks ...

    if row.ArchivedAt != nil && s.objStore != nil {
        rehydratedRow, err := s.rehydrateFromArchive(ctx, row)
        if err == nil {
            row = rehydratedRow
            warnings = append(warnings, "statement rehydrated from cold storage; latency may be higher")
        } else {
            warnings = append(warnings, "failed to rehydrate: " + err.Error())
            // Graceful degradation: return stub with warning
        }
    }

    // Return detail (hydrated or stub with warning)
}
```

**Rehydration Behavior:**

- **Happy path**: Returns full statement with latency warning (~100-500ms)
- **Cache miss**: Retrieves from S3, updates DB cache for next read
- **S3 failure**: Returns stub with fields as NULL, includes warning
- **No object store**: Returns stub without attempting rehydration

#### 4. Repository Updates (`internal/repository/`)

**Models (`models.go`):**

```go
type StatementRow struct {
    // ... existing fields ...
    ArchivedAt *time.Time  // NULL if active, timestamp if archived
    ArchiveKey string      // S3 path if archived, empty if active
}
```

**Interface (`interfaces.go`):**

```go
type StatementRepository interface {
    FindByID(ctx context.Context, id string) (*StatementRow, error)
    ListByCustomerID(...) ([]*StatementRow, int, error)
    UpdateArchivedData(ctx context.Context, id string, stmt *StatementRow) error
}
```

**Mock (`mock.go`):**

- `NewMockStatementRepo()`: Pre-populated with test data
- `UpdateArchivedData()`: Updates archive fields for rehydration cache

## Latency Contract

### Read Latencies

| Scenario                  | Latency   | Notes                                                 |
| ------------------------- | --------- | ----------------------------------------------------- |
| **Active statement**      | <1ms      | Direct Postgres; indexed; hot cache                   |
| **Archived (cache hit)**  | <1ms      | Postgres with populated fields; previously rehydrated |
| **Archived (cache miss)** | 100-500ms | S3 GET + parse JSON; typical for cold storage         |
| **Archived (S3 timeout)** | ~30s      | Retries, context timeout, returns stub                |
| **Rehydration failure**   | <1s       | Attempts S3 once, falls back to stub                  |

### Caller Expectations

1. **Frequent reads** (subscriptions, dashboards): Use active statements; fast
2. **Audit/compliance**: May rehydrate archived statements; expect warnings
3. **Bulk export**: Cache rehydrated statements in-memory; don't re-fetch

## Security & Compliance

### Access Control

1. **RBAC enforcement BEFORE rehydration**: Auth checks happen before S3 lookup
   - If caller lacks permission, returns `ErrForbidden` (no S3 access)
   - Prevents side-channel leaks via timing

2. **Object storage permissions**:
   - Use S3 bucket policies to limit access to service role
   - Encrypt at rest (S3 SSE-S3 or KMS)
   - Encrypt in transit (TLS 1.2+)

### Data Consistency

1. **Constraint enforcement**: `check_archive_consistency` prevents partial archive
2. **Transactional archival**: S3 + DB update both succeed or both fail
3. **Audit trail**: `archived_at` timestamp + object versioning

### Soft-Delete Handling

- Archived statements maintain `DeletedAt` field
- Archival checks `WHERE deleted_at IS NULL`
- Rehydration does not resurrect soft-deleted statements

## Testing Strategy

### Unit Tests

1. **Object Store (`internal/cache/memory_object_store_test.go`)**:
   - Put/Get/Delete operations
   - Data isolation (copy-on-write)
   - Context cancellation
   - Concurrent access

2. **Archive Job (`internal/worker/statement_archive_job_test.go`)**:
   - Batch archival of old statements
   - Payload serialization
   - Idempotency (re-running doesn't duplicate)
   - Health checks
   - Statistics

3. **Statement Service (`internal/service/statement_archive_test.go`)**:
   - Rehydration from cold storage
   - Cache updates after rehydration
   - RBAC with archived statements
   - Graceful degradation (S3 failures, missing objects)
   - Context timeouts

### Integration Tests

```bash
# Run all statement-related tests
go test ./internal/service/... ./internal/worker/... ./internal/cache/...

# With coverage
go test -cover ./internal/service/... ./internal/worker/... ./internal/cache/...

# Coverage report
go test -coverprofile=coverage.out ./internal/service/... ./internal/worker/... ./internal/cache/...
go tool cover -html=coverage.out
```

### Test Coverage Goals

- **Unit tests**: ≥95% statement service + worker + cache
- **Edge cases**:
  - Empty batches (no old statements)
  - Partial failures (S3 succeeds, DB fails)
  - Rehydration cache miss/hit cycle
  - Soft-deleted statements excluded

## Deployment & Rollout

### Phase 1: Database Migration

```bash
# Apply migration
flyway migrate -locations=filesystem:./migrations

# Verify indexes created
SELECT * FROM pg_indexes WHERE schemaname='public' AND tablename='statements';
```

### Phase 2: Staging

1. Deploy `StatementArchiveJob` (starts immediately)
2. Deploy updated `StatementService` with archival support
3. Deploy updated repository + models
4. Run full test suite: `go test ./...` (>95% coverage)
5. Observe: Check job stats, verify no rehydration errors

### Phase 3: Production

1. Deploy with feature flag: archival disabled initially
2. Enable archival on statements >24 months (after 24h burn-in)
3. Monitor:
   - Archive job: archived_count, failed_count, last_run_error
   - Service: rehydration latency, warning frequency, errors
   - S3: PUT success rate, GET latency, storage growth

## Operational Runbook

### Checking Archive Status

```sql
-- Count archived vs. active statements
SELECT archived_at IS NOT NULL as archived, COUNT(*) FROM statements GROUP BY 1;

-- Find statements archived in last 7 days
SELECT id, subscription_id, customer_id, archived_at, archive_key
FROM statements
WHERE archived_at IS NOT NULL
  AND archived_at > NOW() - INTERVAL '7 days'
ORDER BY archived_at DESC
LIMIT 100;

-- Verify archive consistency
SELECT id,
       (archived_at IS NULL AND archive_key IS NULL) as consistent_active,
       (archived_at IS NOT NULL AND archive_key IS NOT NULL) as consistent_archived
FROM statements
WHERE NOT ((archived_at IS NULL AND archive_key IS NULL) OR
           (archived_at IS NOT NULL AND archive_key IS NOT NULL));
```

### Troubleshooting Rehydration

| Symptom                                | Diagnosis                       | Remedy                                 |
| -------------------------------------- | ------------------------------- | -------------------------------------- |
| Rehydration warnings frequent          | S3 latency high or errors       | Check S3 metrics, retry logic          |
| Archived statement missing from S3     | Premature cleanup or corruption | Restore from backup, re-archive        |
| DB cache not updated after rehydration | Service crashed mid-rehydration | Best-effort; retry GET via rehydration |
| RBAC bypass attempt                    | Cached stub returned            | Confirm RBAC checks run before S3      |

### Manual Rehydration

If needed to refresh cached data:

```go
// In internal/service/statement_service.go
// Add manual rehydration method:
func (s *statementService) RehydrateManual(ctx context.Context, statementID string) error {
    row, err := s.stmtRepo.FindByID(ctx, statementID)
    if err != nil { return err }
    if row.ArchivedAt == nil { return errors.New("not archived") }

    hydrated, err := s.rehydrateFromArchive(ctx, row)
    if err != nil { return err }

    return s.stmtRepo.UpdateArchivedData(ctx, statementID, hydrated)
}
```

## Future Enhancements

1. **S3 Adapter**: Implement `aws-sdk-go-v2` backend for production
2. **Tiered Archival**: Move to Glacier after 1 year (cheaper)
3. **Batch Rehydration**: Prefetch related statements on read
4. **Selective Restoration**: Admin endpoint to restore statements back to hot storage
5. **Compression**: GZIP archived payloads to reduce S3 costs

## References

- Migration: `migrations/0010_add_statement_archival.up.sql`
- Code: `internal/worker/statement_archive_job.go`
- Service: `internal/service/statement_service.go`
- Cache: `internal/cache/object_store.go`
- Tests: `*_archive_test.go`, `*_object_store_test.go`
