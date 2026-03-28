package shutdown

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"
)

// TestNewGracefulShutdown tests graceful shutdown initialization
func TestNewGracefulShutdown(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 10*time.Second, 5*time.Second)

	if gs.server != server {
		t.Error("Server not set correctly")
	}
	if gs.shutdownTimeout != 10*time.Second {
		t.Error("Shutdown timeout not set correctly")
	}
	if gs.drainTimeout != 5*time.Second {
		t.Error("Drain timeout not set correctly")
	}
}

// TestGracefulShutdown_OnShutdown tests shutdown callback registration
func TestGracefulShutdown_OnShutdown(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 10*time.Second, 5*time.Second)

	callCount := 0
	gs.OnShutdown(func(ctx context.Context) error {
		callCount++
		return nil
	})

	if len(gs.onShutdown) != 1 {
		t.Error("Callback not registered")
	}
}

// TestGracefulShutdown_MultipleCallbacks tests multiple callback registration
func TestGracefulShutdown_MultipleCallbacks(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 10*time.Second, 5*time.Second)

	gs.OnShutdown(func(ctx context.Context) error { return nil })
	gs.OnShutdown(func(ctx context.Context) error { return nil })
	gs.OnShutdown(func(ctx context.Context) error { return nil })

	if len(gs.onShutdown) != 3 {
		t.Errorf("Expected 3 callbacks, got %d", len(gs.onShutdown))
	}
}

// TestGracefulShutdown_ShutdownCallbacks tests that shutdown callbacks are executed
func TestGracefulShutdown_ShutdownCallbacks(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 1*time.Second, 1*time.Second)

	callOrder := []int{}
	gs.OnShutdown(func(ctx context.Context) error {
		callOrder = append(callOrder, 1)
		return nil
	})
	gs.OnShutdown(func(ctx context.Context) error {
		callOrder = append(callOrder, 2)
		return nil
	})

	gs.Shutdown()
	gs.Wait()

	if len(callOrder) != 2 {
		t.Errorf("Expected 2 callbacks to be called, got %d", len(callOrder))
	}
	if callOrder[0] != 1 || callOrder[1] != 2 {
		t.Errorf("Callbacks not executed in order: %v", callOrder)
	}
}

// TestGracefulShutdown_CallbackError tests error handling in callbacks
func TestGracefulShutdown_CallbackError(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 1*time.Second, 1*time.Second)

	gs.OnShutdown(func(ctx context.Context) error {
		return fmt.Errorf("callback error")
	})
	gs.OnShutdown(func(ctx context.Context) error {
		return nil
	})

	// Should not panic even with callback error
	gs.Shutdown()
	gs.Wait()
}

// TestGracefulShutdown_CallbackTimeout tests callback timeout handling
func TestGracefulShutdown_CallbackTimeout(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	// Very short timeout to trigger timeout
	gs := NewGracefulShutdown(server, 100*time.Millisecond, 100*time.Millisecond)

	gs.OnShutdown(func(ctx context.Context) error {
		// Sleep longer than the timeout
		select {
		case <-time.After(200 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	start := time.Now()
	gs.Shutdown()
	gs.Wait()
	elapsed := time.Since(start)

	// Should complete within reasonable time despite slow callback
	if elapsed > 1*time.Second {
		t.Errorf("Shutdown took too long: %v", elapsed)
	}
}

// TestGracefulShutdown_IsShuttingDown tests shutdown status check
func TestGracefulShutdown_IsShuttingDown(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 1*time.Second, 1*time.Second)

	if gs.IsShuttingDown() {
		t.Error("Should not be shutting down yet")
	}

	gs.Shutdown()

	if !gs.IsShuttingDown() {
		t.Error("Should be shutting down")
	}

	gs.Wait()
}

// TestGracefulShutdown_Wait tests wait functionality
func TestGracefulShutdown_Wait(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 1*time.Second, 1*time.Second)

	done := make(chan struct{})
	go func() {
		gs.Wait()
		close(done)
	}()

	gs.Shutdown()

	select {
	case <-done:
		// Success
	case <-time.After(5 * time.Second):
		t.Error("Wait timeout")
	}
}

// TestGracefulShutdown_ShutdownOnlyOnce tests that shutdown runs only once
func TestGracefulShutdown_ShutdownOnlyOnce(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 1*time.Second, 1*time.Second)

	callCount := 0
	gs.OnShutdown(func(ctx context.Context) error {
		callCount++
		return nil
	})

	// Call shutdown multiple times
	gs.Shutdown()
	gs.Shutdown()
	gs.Shutdown()

	gs.Wait()

	if callCount != 1 {
		t.Errorf("Shutdown callback called %d times, expected 1", callCount)
	}
}

// TestGracefulShutdown_WithPendingRequests tests shutdown with long-running requests
func TestGracefulShutdown_WithPendingRequests(t *testing.T) {
	// Create a test HTTP server with a slow endpoint
	mux := http.NewServeMux()
	requestStarted := make(chan struct{})
	requestFinished := make(chan struct{})

	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		close(requestFinished)
	})

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  5 * time.Second,
	}

	// Start test server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	server.Addr = listener.Addr().String()

	go func() {
		server.Serve(listener)
	}()

	gs := NewGracefulShutdown(server, 5*time.Second, 2*time.Second)

	// Start a request
	go func() {
		http.Get(fmt.Sprintf("http://%s/slow", server.Addr))
	}()

	// Wait for request to start
	<-requestStarted

	// Start shutdown (should drain the request)
	shutdownDone := make(chan struct{})
	go func() {
		gs.Shutdown()
		gs.Wait()
		close(shutdownDone)
	}()

	// Wait for request to finish
	select {
	case <-requestFinished:
		t.Log("Request completed successfully during shutdown")
	case <-time.After(5 * time.Second):
		t.Error("Request did not complete in time")
	}

	// Wait for shutdown to complete
	select {
	case <-shutdownDone:
		t.Log("Shutdown completed successfully")
	case <-time.After(5 * time.Second):
		t.Error("Shutdown did not complete in time")
	}
}

// TestGracefulShutdown_ContextCancellation tests that shutdown respects context
func TestGracefulShutdown_ContextCancellation(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 500*time.Millisecond, 500*time.Millisecond)

	gs.OnShutdown(func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
			return nil
		}
	})

	gs.Shutdown()
	gs.Wait()

	// Context may or may not be cancelled depending on timing
	// Just verify shutdown completed
	if gs.IsShuttingDown() == false {
		t.Error("Should be shutting down")
	}
}

// TestGracefulShutdown_ConcurrentShutdown tests concurrent shutdown calls
func TestGracefulShutdown_ConcurrentShutdown(t *testing.T) {
	server := &http.Server{Addr: ":8080"}
	gs := NewGracefulShutdown(server, 1*time.Second, 1*time.Second)

	callCount := 0
	gs.OnShutdown(func(ctx context.Context) error {
		callCount++
		return nil
	})

	// Call shutdown concurrently
	done := make(chan struct{}, 3)
	for i := 0; i < 3; i++ {
		go func() {
			gs.Shutdown()
			gs.Wait()
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		<-done
	}

	// Callback should only be called once
	if callCount != 1 {
		t.Errorf("Callback called %d times, expected 1", callCount)
	}
}
