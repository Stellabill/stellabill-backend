package worker

import (
	"context"
	"errors"
	"testing"
)

type fakeLeaderLocker struct {
	acquired      bool
	acquireResult bool
	acquireErr    error
	releaseErr    error
	releases      int
}

func (f *fakeLeaderLocker) AcquireLock(ctx context.Context, key int64) (bool, error) {
	if f.acquireErr != nil {
		return false, f.acquireErr
	}
	f.acquired = f.acquireResult
	return f.acquireResult, nil
}

func (f *fakeLeaderLocker) ReleaseLock(ctx context.Context, key int64) error {
	if f.releaseErr != nil {
		return f.releaseErr
	}
	f.releases++
	f.acquired = false
	return nil
}

func (f *fakeLeaderLocker) Close() error {
	return nil
}

func TestLeaderLockerAcquireAndRelease(t *testing.T) {
	locker := &fakeLeaderLocker{acquireResult: true}

	acquired, err := locker.AcquireLock(context.Background(), 123)
	if err != nil {
		t.Fatalf("AcquireLock returned error: %v", err)
	}
	if !acquired {
		t.Fatal("expected lock acquisition to succeed")
	}

	if err := locker.ReleaseLock(context.Background(), 123); err != nil {
		t.Fatalf("ReleaseLock returned error: %v", err)
	}
	if locker.releases != 1 {
		t.Fatalf("expected one release, got %d", locker.releases)
	}
}

func TestLeaderGuardRunWhenLockIsNotAvailable(t *testing.T) {
	locker := &fakeLeaderLocker{acquireResult: false}
	guard := newLeaderGuard(locker, "test-job", 42)

	err := guard.Run(context.Background(), func(ctx context.Context) error {
		t.Fatal("callback should not run when leadership is not acquired")
		return nil
	})
	if !errors.Is(err, errNotLeader) {
		t.Fatalf("expected errNotLeader, got %v", err)
	}
}

func TestLeaderGuardRunReleasesLockAfterCallback(t *testing.T) {
	locker := &fakeLeaderLocker{acquireResult: true}
	guard := newLeaderGuard(locker, "test-job", 42)

	err := guard.Run(context.Background(), func(ctx context.Context) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if locker.releases != 1 {
		t.Fatalf("expected one release after callback, got %d", locker.releases)
	}
}
