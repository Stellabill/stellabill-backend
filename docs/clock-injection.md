# Clock injection for deterministic time-sensitive tests

`internal/timeutil.Clock` abstracts wall-clock reads so job scheduling, TTL
expiry, and archival-cutoff logic can be tested deterministically instead of
depending on `time.Now()` and real sleeps.

```go
type Clock interface {
    Now() time.Time // always normalized to UTC
}
```

- `timeutil.SystemClock` is the production implementation (backed by
  `timeutil.NowUTC()`). It's the default for every constructor below — no
  behavior changes for existing callers.
- `timeutil.NewFakeClock(start)` returns a `*FakeClock` for tests, with
  `Set(t)` to jump to an absolute time and `Advance(d)` to move forward (or
  backward, for negative `d`) by a duration. Both operate on the absolute
  instant, so they behave correctly across daylight-saving-time transitions
  even if seeded from a non-UTC `time.Time`.

## Where it's wired in

| Type | Constructor | Test hook |
|---|---|---|
| `worker.Scheduler` | `NewScheduler(store)` | `(*Scheduler).SetClock(c)` |
| `worker.MemoryStore` | `NewMemoryStore()` | `NewMemoryStoreWithClock(c)` |
| `worker.StatementArchiveJob` | `NewStatementArchiveJob(...)` | `(*StatementArchiveJob).SetClock(c)` |
| `worker.FeeRevenueRefreshJob` | `NewFeeRevenueRefreshJob(...)` | `(*FeeRevenueRefreshJob).SetClock(c)` |

Call `SetClock`/use the `WithClock` constructor **before** `Start()` — these
jobs read the clock field from their own background goroutine, so setting it
before the goroutine is spawned is race-free without extra locking.

## Example

```go
clock := timeutil.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

store := worker.NewMemoryStoreWithClock(clock)
_, _ = store.AcquireLock("job-1", "worker-a", 5*time.Minute)

clock.Advance(5*time.Minute + time.Second) // past the TTL
ok, _ := store.AcquireLock("job-1", "worker-b", 5*time.Minute) // ok == true
```

## Not migrated

`internal/service/quiet_hours.go`'s `IsQuietHours()` is an unimplemented
stub (`return false`, no time comparison, no callers) — there is no
time-dependent logic there yet to inject a clock into. When quiet-hours
enforcement is implemented against `model.NotificationPreferences`'
`QuietStart`/`QuietEnd`/`Timezone` fields, it should take a `timeutil.Clock`
(or an explicit `now time.Time`) as a parameter from the start, following the
pattern above.
