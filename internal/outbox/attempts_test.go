package outbox

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestSaveAndListAttempts(t *testing.T) {
	repo := NewMemAttemptRepository()
	eventID := uuid.New()
	code := 201

	err := repo.SaveAttempt(&Attempt{
		EventID:       eventID,
		TenantID:      "t1",
		AttemptNumber: 1,
		ResponseCode:  &code,
		AttemptedAt:   time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("SaveAttempt: %v", err)
	}

	list, err := repo.ListAttempts("t1", eventID)
	if err != nil {
		t.Fatalf("ListAttempts: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 attempt, got %d", len(list))
	}
}

func TestListAttempts_TenantIsolation(t *testing.T) {
	repo := NewMemAttemptRepository()
	eventID := uuid.New()

	_ = repo.SaveAttempt(&Attempt{
		EventID: eventID, TenantID: "t1", AttemptNumber: 1, AttemptedAt: time.Now().UTC(),
	})

	list, _ := repo.ListAttempts("t2", eventID)
	if len(list) != 0 {
		t.Fatalf("expected 0 for different tenant, got %d", len(list))
	}
}

func TestListAttempts_OrderNewestFirst(t *testing.T) {
	repo := NewMemAttemptRepository()
	eventID := uuid.New()
	now := time.Now().UTC()

	_ = repo.SaveAttempt(&Attempt{EventID: eventID, TenantID: "t1", AttemptNumber: 1, AttemptedAt: now.Add(-2 * time.Second)})
	_ = repo.SaveAttempt(&Attempt{EventID: eventID, TenantID: "t1", AttemptNumber: 2, AttemptedAt: now})

	list, _ := repo.ListAttempts("t1", eventID)
	if list[0].AttemptNumber != 2 {
		t.Fatalf("expected newest first (attempt 2), got %d", list[0].AttemptNumber)
	}
}

func TestSaveAttempt_IDAutoAssigned(t *testing.T) {
	repo := NewMemAttemptRepository()
	a := &Attempt{EventID: uuid.New(), TenantID: "t1", AttemptNumber: 1}
	_ = repo.SaveAttempt(a)
	if a.ID == uuid.Nil {
		t.Fatal("expected ID to be auto-assigned")
	}
}

func TestSaveAttempt_NilReturnsError(t *testing.T) {
	repo := NewMemAttemptRepository()
	if err := repo.SaveAttempt(nil); err == nil {
		t.Fatal("expected error for nil attempt")
	}
}

func TestTruncateAndScrubBody_Truncates(t *testing.T) {
	big := strings.Repeat("x", 5000)
	result := TruncateAndScrubBody(big)
	if len(result) > maxResponseBodyBytes {
		t.Fatalf("body not truncated: len=%d", len(result))
	}
}

func TestTruncateAndScrubBody_ShortBodyUnchanged(t *testing.T) {
	body := "hello"
	if TruncateAndScrubBody(body) != body {
		t.Fatal("short body should be unchanged")
	}
}

func TestSaveAttempt_ResponseBodyTruncated(t *testing.T) {
	repo := NewMemAttemptRepository()
	big := strings.Repeat("y", 5000)
	a := &Attempt{
		EventID: uuid.New(), TenantID: "t1", AttemptNumber: 1,
		ResponseBody: &big,
	}
	_ = repo.SaveAttempt(a)

	list, _ := repo.ListAttempts("t1", a.EventID)
	if len(*list[0].ResponseBody) > maxResponseBodyBytes {
		t.Fatalf("response body not truncated on save")
	}
}
