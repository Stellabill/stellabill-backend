package worker

import (
	"context"
	"fmt"
	"testing"
)

// Example test showing how to use the worker in integration tests
func TestWorkerExample(t *testing.T) {
	// This is just a skeletal example for documentation/integration purposes
	// In a real test, you'd use a real or mock JobStore and JobExecutor
}

type exampleMockExecutor struct {
	executed bool
}

func (m *exampleMockExecutor) Execute(ctx context.Context, job *Job) error {
	m.executed = true
	fmt.Printf("Executing job: %s\n", job.ID)
	return nil
}
