# Database Migration Guide

To ensure database schema integrity and prevent accidental modification of merged migrations, this project enforces a checksum-based guardrail in CI.

## Adding a New Migration

1. Create your migration files in the `migrations/` directory:
   - `NNN_description.up.sql`
   - `NNN_description.down.sql` (optional but recommended)
   - Alternatively: `NNN_description.sql`

2. After creating the files, update the checksum registry by running the check script locally:
   ```bash
   go run ./scripts/check-migrations.go
   ```
   The script will detect new files and update `migrations/checksums.json`.

3. Commit both your new migration files AND the updated `migrations/checksums.json`.

## Merged Migrations are Immutable

Once a migration is merged and its checksum is recorded in `migrations/checksums.json`, it **must not be edited or removed**.

If you need to change the schema defined by an existing migration, you must **create a new migration** that applies the desired changes.

CI will fail if:
- An existing migration file listed in `checksums.json` is modified.
- An existing migration file listed in `checksums.json` is deleted.
- New migration files are added without updating `checksums.json`.

## Bypassing (Not Recommended)

If you absolutely must modify an existing migration (e.g., to fix a severe bug in a migration that hasn't been deployed yet), you must manually update its checksum in `migrations/checksums.json` or delete the entry and run the check script to regenerate it.
