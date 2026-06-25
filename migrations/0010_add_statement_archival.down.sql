-- Rollback archival support
ALTER TABLE statements
DROP CONSTRAINT IF EXISTS check_archive_consistency;

DROP INDEX IF EXISTS idx_statements_archival_scan;
DROP INDEX IF EXISTS idx_statements_active_id;

ALTER TABLE statements
DROP COLUMN IF EXISTS archived_at,
DROP COLUMN IF EXISTS archive_key;
