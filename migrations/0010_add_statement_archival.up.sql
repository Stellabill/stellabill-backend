-- Add columns to support statement archival to cold storage.
-- - archived_at: timestamp when statement was archived (NULL means not archived, active row)
-- - archive_key: S3-like path to archived statement JSON (only populated if archived_at is set)
--
-- Archival strategy:
-- 1. Statements older than 24 months are candidates for archival
-- 2. When archived, full statement data is serialized to JSON and stored in object storage
-- 3. The row is replaced with a stub containing only archive_key, archive_at, and minimal metadata
-- 4. Reads transparently rehydrate from object storage on cache miss
-- 5. archive_at prevents accidental re-archival and serves as audit trail

ALTER TABLE statements
ADD COLUMN archived_at TIMESTAMPTZ,
ADD COLUMN archive_key TEXT;

-- Create index for efficient archival job scanning:
-- Queries statements older than 24 months that haven't been archived yet
CREATE INDEX IF NOT EXISTS idx_statements_archival_scan 
ON statements (issued_at ASC) 
WHERE archived_at IS NULL AND deleted_at IS NULL;

-- Ensure archive_key and archived_at are mutually consistent:
-- Either both are NULL (active row) or both are set (archived row)
ALTER TABLE statements
ADD CONSTRAINT check_archive_consistency 
CHECK (
  (archived_at IS NULL AND archive_key IS NULL) OR
  (archived_at IS NOT NULL AND archive_key IS NOT NULL)
);

-- Optional: Create index for rehydration cache miss lookups
-- Queries active (non-archived) statements by ID
CREATE INDEX IF NOT EXISTS idx_statements_active_id 
ON statements (id) 
WHERE archived_at IS NULL AND deleted_at IS NULL;
