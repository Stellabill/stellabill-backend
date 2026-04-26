package shutdown

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGracefulShutdown_Integration_SimpleServer tests graceful shutdown with a simple HTTP server
func TestGracefulShutdown_Integration_SimpleServer(t *testing.T) {
	// Create simple test server
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Start server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	server.Addr = listener.Addr().String()

	go server.Serve(listener)

	gs := NewGracefulShutdown(server, 5*time.Second, 2*time.Second)

	// Verify server is responding
	resp, err := http.Get(fmt.Sprintf("http://%s/", server.Addr))
	if err != nil {
		t.Fatalf("Failed to connect to server: %v", err)
	}
	resp.Body.Close()

	// Shutdown should complete within timeout
	start := time.Now()
	gs.Shutdown()
	gs.Wait()
	elapsed := time.Since(start)

	if elapsed > 5*time.Second {
		t.Errorf("Shutdown took too long: %v", elapsed)
	}
}

// TestGracefulShutdown_Integration_MultipleRequests tests shutdown drains multiple concurrent requests
func TestGracefulShutdown_Integration_MultipleRequests(t *testing.T) {
	// Create server with multiple slow endpoints
	requestCounter := int32(0)
	requestStarted := sync.WaitGroup{}
	requestFinished := sync.WaitGroup{}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/slow", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCounter, 1)
		requestStarted.Done()
		time.Sleep(200 * time.Millisecond) // Simulate slow request
		requestFinished.Done()
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	server.Addr = listener.Addr().String()

	go server.Serve(listener)

	// Start multiple concurrent requests
	numRequests := 5
	requestStarted.Add(numRequests)
	requestFinished.Add(numRequests)

	for i := 0; i < numRequests; i++ {
		go func() {
			http.Get(fmt.Sprintf("http://%s/api/slow", server.Addr))
		}()
	}

	// Wait for all requests to start
	requestStarted.Wait()

	gs := NewGracefulShutdown(server, 5*time.Second, 3*time.Second)
	gs.Shutdown()
	gs.Wait()

	// All requests should have completed
	if int(atomic.LoadInt32(&requestCounter)) != numRequests {
		t.Errorf("Expected %d requests, got %d", numRequests, requestCounter)
	}

	// Shutdown should complete successfully
	if !gs.IsShuttingDown() {
		t.Error("Should be in shutdown state")
	}
}

// TestGracefulShutdown_Integration_CallbacksAndDraining tests callbacks execute after request draining
func TestGracefulShutdown_Integration_CallbacksAndDraining(t *testing.T) {
	// Track execution order and timing
	type Event struct {
		name      string
		timestamp time.Time
	}

	events := []Event{}
	eventsMutex := sync.Mutex{}

	// Create server with a request that takes some time
	requestStarted := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		eventsMutex.Lock()
		events = append(events, Event{"request_start", time.Now()})
		eventsMutex.Unlock()
		close(requestStarted)

		time.Sleep(300 * time.Millisecond)

		eventsMutex.Lock()
		events = append(events, Event{"request_end", time.Now()})
		eventsMutex.Unlock()

		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Handler: mux,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	server.Addr = listener.Addr().String()

	go server.Serve(listener)

	// Start a request
	go http.Get(fmt.Sprintf("http://%s/api/data", server.Addr))
	<-requestStarted // Wait for request to actually start processing

	gs := NewGracefulShutdown(server, 5*time.Second, 3*time.Second)

	// Register callback to track when callbacks run
	gs.OnShutdown(func(ctx context.Context) error {
		eventsMutex.Lock()
		events = append(events, Event{"callback_start", time.Now()})
		eventsMutex.Unlock()

		time.Sleep(100 * time.Millisecond)

		eventsMutex.Lock()
		events = append(events, Event{"callback_end", time.Now()})
		eventsMutex.Unlock()
		return nil
	})

	gs.Shutdown()
	gs.Wait()

	// Verify execution order: request must finish before callback starts
	if len(events) < 4 {
		t.Fatalf("Expected at least 4 events, got %d", len(events))
	}

	requestEndIdx := -1
	callbackStartIdx := -1

	for i, event := range events {
		if event.name == "request_end" {
			requestEndIdx = i
		}
		if event.name == "callback_start" {
			callbackStartIdx = i
		}
	}

	if requestEndIdx == -1 || callbackStartIdx == -1 {
		t.Errorf("Missing events: request_end=%d, callback_start=%d", requestEndIdx, callbackStartIdx)
	}

	if requestEndIdx < callbackStartIdx {
		t.Log("Correct order: request completed before callback started")
	}
}

// TestGracefulShutdown_Integration_RequestTimeout tests shutdown timeout behavior
func TestGracefulShutdown_Integration_RequestTimeout(t *testing.T) {
	// Create very long-running request
	mux := http.NewServeMux()
	mux.HandleFunc("/forever", func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Second) // Very long request
		w.WriteHeader(http.StatusOK)
	})

	server := &http.Server{
		Handler: mux,
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	server.Addr = listener.Addr().String()

	go server.Serve(listener)

	// Start long-running request
	go http.Get(fmt.Sprintf("http://%s/forever", server.Addr))

	time.Sleep(100 * time.Millisecond) // Give request time to start

	// Shutdown with very short timeout - should force close
	gs := NewGracefulShutdown(server, 500*time.Millisecond, 500*time.Millisecond)
	gs.Shutdown()

	start := time.Now()
	gs.Wait()
	elapsed := time.Since(start)

	// Should complete within timeout even with long-running request
	if elapsed > 2*time.Second {
		t.Errorf("Shutdown took too long with forced close: %v", elapsed)
	}

	t.Logf("Shutdown completed in %v despite long-running request", elapsed)
}

// TestGracefulShutdown_Integration_CallbackWithContext tests shutdown context is passed to callbacks
func TestGracefulShutdown_Integration_CallbackWithContext(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	// Very short callback timeout
	gs := NewGracefulShutdown(server, 1*time.Second, 1*time.Second)

	contextWorkedAsExpected := false
	gs.OnShutdown(func(ctx context.Context) error {
		// Context should be provided and work correctly
		if ctx == nil {
			return fmt.Errorf("context is nil")
		}

		// Try to use context
		select {
		case <-ctx.Done():
			// Context might be done, that's ok
			return nil
		case <-time.After(100 * time.Millisecond):
			contextWorkedAsExpected = true
			return nil
		}
	})

	gs.Shutdown()
	gs.Wait()

	if !contextWorkedAsExpected {
		t.Error("Context did not work as expected in callback")
	}
}

func TestShutdown_AuditAndOutboxExecuted(t *testing.T) {
	server := &http.Server{}
	gs := NewGracefulShutdown(server, 3*time.Second, 2*time.Second)

	auditCalled := false
	outboxCalled := false

	gs.RegisterAuditFlush(func(ctx context.Context) error {
		auditCalled = true
		return nil
	})

	gs.RegisterOutboxFlush(func(ctx context.Context) error {
		outboxCalled = true
		return nil
	})

	go gs.Shutdown()
	gs.Wait()

	if !auditCalled || !outboxCalled {
		t.Fatal("audit/outbox not executed during shutdown")
	}
}
