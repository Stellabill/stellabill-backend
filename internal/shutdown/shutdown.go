package shutdown

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// GracefulShutdown orchestrates clean server shutdown with request draining
type GracefulShutdown struct {
	server              *http.Server
	shutdownTimeout     time.Duration
	drainTimeout        time.Duration
	onShutdown          []func(context.Context) error
	shutdownOnce        sync.Once
	shutdownComplete    chan struct{}
	forcedTerminationCh chan struct{}
}

// NewGracefulShutdown creates a new graceful shutdown orchestrator
func NewGracefulShutdown(server *http.Server, shutdownTimeout, drainTimeout time.Duration) *GracefulShutdown {
	return &GracefulShutdown{
		server:              server,
		shutdownTimeout:     shutdownTimeout,
		drainTimeout:        drainTimeout,
		onShutdown:          []func(context.Context) error{},
		shutdownComplete:    make(chan struct{}),
		forcedTerminationCh: make(chan struct{}),
	}
}

// OnShutdown registers a callback to be executed during shutdown
// Callbacks are executed in the order they were registered
func (gs *GracefulShutdown) OnShutdown(fn func(context.Context) error) {
	gs.onShutdown = append(gs.onShutdown, fn)
}

// ListenForShutdownSignals starts listening for OS signals and triggers graceful shutdown
// This function blocks until shutdown is complete
func (gs *GracefulShutdown) ListenForShutdownSignals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Start goroutine to handle signals
	go func() {
		sig := <-sigCh
		log.Printf("Received signal: %v", sig)
		gs.Shutdown()
	}()

	// Wait for shutdown to complete
	<-gs.shutdownComplete
}

// Shutdown triggers graceful shutdown orchestration
// It drains in-flight requests, executes cleanup callbacks, and closes the server
func (gs *GracefulShutdown) Shutdown() {
	gs.shutdownOnce.Do(func() {
		defer close(gs.shutdownComplete)

		log.Println("Starting graceful shutdown...")

		// Phase 1: Request draining with timeout
		drainCtx, drainCancel := context.WithTimeout(context.Background(), gs.drainTimeout)
		defer drainCancel()

		log.Printf("Phase 1: Draining in-flight requests (timeout: %v)...", gs.drainTimeout)
		if err := gs.drainRequests(drainCtx); err != nil {
			log.Printf("Warning: Request draining error: %v", err)
		}

		// Phase 2: Shutdown callbacks with timeout
		callbackCtx, callbackCancel := context.WithTimeout(context.Background(), gs.shutdownTimeout)
		defer callbackCancel()

		log.Printf("Phase 2: Executing shutdown callbacks (timeout: %v)...", gs.shutdownTimeout)
		gs.executeShutdownCallbacks(callbackCtx)

		// Phase 3: Close HTTP server
		log.Println("Phase 3: Closing HTTP server...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()

		if err := gs.server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Error during server shutdown: %v", err)
			// Force close if graceful shutdown fails
			if err := gs.server.Close(); err != nil {
				log.Printf("Error forcing server close: %v", err)
			}
		}

		log.Println("Graceful shutdown completed")
	})
}

// drainRequests stops accepting new requests and waits for in-flight requests to complete
func (gs *GracefulShutdown) drainRequests(ctx context.Context) error {
	// Stop accepting new connections
	log.Println("Stopping acceptance of new requests...")

	// Create a channel to signal when all requests are drained
	drained := make(chan struct{})

	// Monitor server shutdown in background
	go func() {
		// Use a custom context that we control
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// This will wait for all in-flight requests to complete
		if err := gs.server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Shutdown returned error: %v", err)
		}
		close(drained)
	}()

	// Wait for either all requests to drain or timeout
	select {
	case <-drained:
		log.Println("All in-flight requests drained successfully")
		return nil
	case <-ctx.Done():
		log.Println("Request drain timeout - forcing close")
		return gs.server.Close()
	}
}

// executeShutdownCallbacks runs all registered shutdown callbacks
func (gs *GracefulShutdown) executeShutdownCallbacks(ctx context.Context) {
	if len(gs.onShutdown) == 0 {
		return
	}

	for i, fn := range gs.onShutdown {
		log.Printf("Executing shutdown callback %d...", i+1)
		if err := fn(ctx); err != nil {
			log.Printf("Shutdown callback %d error: %v", i+1, err)
		}
	}

	log.Println("All shutdown callbacks completed")
}

// Wait blocks until shutdown is complete
func (gs *GracefulShutdown) Wait() {
	<-gs.shutdownComplete
}

// IsShuttingDown returns true if shutdown has been initiated
func (gs *GracefulShutdown) IsShuttingDown() bool {
	select {
	case <-gs.shutdownComplete:
		return true
	default:
		return false
	}
}
