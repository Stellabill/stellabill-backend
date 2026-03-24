package worker

import (
	"testing"
	"time"
)

func TestScheduler_ScheduleCharge(t *testing.T) {
	store := NewMemoryStore()
	scheduler := NewScheduler(store)
	
	scheduledAt := time.Now().Add(1 * time.Hour)
	job, err := scheduler.ScheduleCharge("sub-1", scheduledAt, 3)
	
	if err != nil {
		t.Fatalf("Failed to schedule charge: %v", err)
	}
	
	if job.Type != "charge" {
		t.Errorf("Expected type 'charge', got %s", job.Type)
	}
	if job.SubscriptionID != "sub-1" {
		t.Errorf("Expected subscription 'sub-1', got %s", job.SubscriptionID)
	}
	if job.Status != JobStatusPending {
		t.Errorf("Expected status %s, got %s", JobStatusPending, job.Status)
	}
	if job.MaxAttempts != 3 {
		t.Errorf("Expected max attempts 3, got %d", job.MaxAttempts)
	}
	
	// Verify job was stored
	retrieved, err := store.Get(job.ID)
	if err != nil {
		t.Fatalf("Failed to retrieve job: %v", err)
	}
	if retrieved.ID != job.ID {
		t.Errorf("Expected ID %s, got %s", job.ID, retrieved.ID)
	}
}

func TestScheduler_ScheduleInvoice(t *testing.T) {
	store := NewMemoryStore()
	scheduler := NewScheduler(store)
	
	scheduledAt := time.Now().Add(1 * time.Hour)
	job, err := scheduler.ScheduleInvoice("sub-2", scheduledAt, 3)
	
	if err != nil {
		t.Fatalf("Failed to schedule invoice: %v", err)
	}
	
	if job.Type != "invoice" {
		t.Errorf("Expected type 'invoice', got %s", job.Type)
	}
	if job.SubscriptionID != "sub-2" {
		t.Errorf("Expected subscription 'sub-2', got %s", job.SubscriptionID)
	}
}

func TestScheduler_ScheduleReminder(t *testing.T) {
	store := NewMemoryStore()
	scheduler := NewScheduler(store)
	
	scheduledAt := time.Now().Add(1 * time.Hour)
	job, err := scheduler.ScheduleReminder("sub-3", scheduledAt, 3)
	
	if err != nil {
		t.Fatalf("Failed to schedule reminder: %v", err)
	}
	
	if job.Type != "reminder" {
		t.Errorf("Expected type 'reminder', got %s", job.Type)
	}
	if job.SubscriptionID != "sub-3" {
		t.Errorf("Expected subscription 'sub-3', got %s", job.SubscriptionID)
	}
}
