## Description
This pull request introduces robust migration safety checks and policies to prevent schema state drift and concurrency issues during deployments. It also resolves a missing down-migration file that was breaking CI pipelines.

### Changes Included
* **Documentation**: Updated `docs/migrations.md` to establish a clear Down-Migration Policy, Migration Locking Guidance, and Rollback Playbooks. Added notes on preserving authentication invariants and database integrity.
* **Validation CI Check**: Implemented a new migration sequence verification tool (`cmd/validate-migrations/main.go`) utilizing `ValidateSequence` added to the `internal/migrations` package.
* **Testing**: Wrote comprehensive unit tests (`internal/migrations/migrations_test.go`) validating that all migrations exactly follow an uninterrupted sequential version pattern.
* **CI Integration**: Hooked up the `validate-migrations` safety check directly into `.github/workflows/ci.yml`.
* **Fix**: Restored CI health and addressed strict validation failures by providing the missing `migrations/0002_create_outbox.down.sql`.

Resolves #136
