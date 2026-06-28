# TODO - feat: per-publisher outbox drain

- [ ] Step 1: Refactor `internal/outbox/dispatcher.go` to remove/disable the global lockstep batch drain (`dispatchLoop/processPendingEvents/processEvent`). Only per-publisher drains should advance progress.
- [ ] Step 2: Verify `drainOnceForPublisher` advances only per-publisher cursor on success and marks global event `completed` only when all publishers have progressed past the event.
- [ ] Step 3: Ensure per-publisher lag metric `outbox_publisher_lag_seconds{publisher=...}` is emitted for every cursor advancement.
- [x] Step 4: Align bounded retry + dead-letter behavior with task requirement (bounded per-publisher failure streak; mark event failed after max retries to terminate endless retry).

- [ ] Step 5: Add/adjust tests in `internal/outbox/*_test.go` for:
  - [ ] mixed publisher success/failure isolation
  - [ ] crash/restart recovery from persisted per-publisher cursors
  - [ ] bounded retry + dead-letter path
- [ ] Step 6: Run `go test ./internal/outbox/... -count=1 -timeout 120s` and record results.

