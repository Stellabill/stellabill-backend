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

// GracefulShutdown manages safe server shutdown
type GracefulShutdown struct {
	server          *http.Server
	shutdownTimeout time.Duration
	drainTimeout    time.Duration

	onShutdown []func(context.Context) error

	// Hooks for safety-critical systems
	flushAuditLogs func(context.Context) error
	flushOutbox    func(context.Context) error

	// Global cancellation propagation
	ctx        context.Context
	cancelFunc context.CancelFunc

	shutdownOnce     sync.Once
	shutdownComplete chan struct{}
}

// NewGracefulShutdown initializes shutdown manager
func NewGracefulShutdown(server *http.Server, shutdownTimeout, drainTimeout time.Duration) *GracefulShutdown {
	ctx, cancel := context.WithCancel(context.Background())

	return &GracefulShutdown{
		server:           server,
		shutdownTimeout:  shutdownTimeout,
		drainTimeout:     drainTimeout,
		onShutdown:       []func(context.Context) error{},
		ctx:              ctx,
		cancelFunc:       cancel,
		shutdownComplete: make(chan struct{}),
	}
}

// Context returns the root context for propagation
func (gs *GracefulShutdown) Context() context.Context {
	return gs.ctx
}

// Register cleanup callback
func (gs *GracefulShutdown) OnShutdown(fn func(context.Context) error) {
	gs.onShutdown = append(gs.onShutdown, fn)
}

// Register audit log flush hook
func (gs *GracefulShutdown) RegisterAuditFlush(fn func(context.Context) error) {
	gs.flushAuditLogs = fn
}

// Register outbox flush hook
func (gs *GracefulShutdown) RegisterOutboxFlush(fn func(context.Context) error) {
	gs.flushOutbox = fn
}

// Listen for OS signals (SIGINT, SIGTERM)
func (gs *GracefulShutdown) ListenForShutdownSignals() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Printf("Received signal: %v", sig)
		gs.Shutdown()
	}()

	<-gs.shutdownComplete
}

// Shutdown executes graceful shutdown sequence
func (gs *GracefulShutdown) Shutdown() {
	gs.shutdownOnce.Do(func() {
		defer close(gs.shutdownComplete)

		log.Println("Starting graceful shutdown...")

		// 🔴 STEP 1: Cancel global context
		if gs.cancelFunc != nil {
			gs.cancelFunc()
		}

		// 🔴 STEP 2: Stop server + drain requests
		drainCtx, drainCancel := context.WithTimeout(context.Background(), gs.drainTimeout)
		defer drainCancel()

		if err := gs.server.Shutdown(drainCtx); err != nil {
			log.Printf("Server shutdown error: %v", err)
			_ = gs.server.Close() // force close
		}

		// 🔴 STEP 3: Flush audit logs
		if gs.flushAuditLogs != nil {
			ctx, cancel := context.WithTimeout(context.Background(), gs.shutdownTimeout)
			defer cancel()

			if err := gs.flushAuditLogs(ctx); err != nil {
				log.Printf("Audit log flush error: %v", err)
			}
		}

		// 🔴 STEP 4: Flush outbox
		if gs.flushOutbox != nil {
			ctx, cancel := context.WithTimeout(context.Background(), gs.shutdownTimeout)
			defer cancel()

			if err := gs.flushOutbox(ctx); err != nil {
				log.Printf("Outbox flush error: %v", err)
			}
		}

		// 🔴 STEP 5: Run shutdown callbacks safely
		cbCtx, cbCancel := context.WithTimeout(context.Background(), gs.shutdownTimeout)
		defer cbCancel()

		for i, fn := range gs.onShutdown {
			log.Printf("Running shutdown callback %d", i+1)

			done := make(chan error, 1)

			go func(f func(context.Context) error) {
				done <- f(cbCtx)
			}(fn)

			select {
			case <-cbCtx.Done():
				log.Println("Callback timeout reached")
			case err := <-done:
				if err != nil {
					log.Printf("Callback error: %v", err)
				}
			}
		}

		log.Println("Graceful shutdown completed")
	})
}

// Wait blocks until shutdown completes
func (gs *GracefulShutdown) Wait() {
	<-gs.shutdownComplete
}

// IsShuttingDown returns true when shutdown is triggered/completed
func (gs *GracefulShutdown) IsShuttingDown() bool {
	select {
	case <-gs.shutdownComplete:
		return true
	default:
		return false
	}
}
