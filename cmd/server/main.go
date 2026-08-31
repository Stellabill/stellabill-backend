package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"stellarbill-backend/internal/db"
	"stellarbill-backend/internal/metrics"
)

// listenAndServe is a package-level variable so tests can inject a fake
// listener without network access.
var listenAndServe = func(srv *http.Server) error {
	return srv.ListenAndServe()
}

func main() {
	pool, replicaPool, srv, err := InitializeServer()
	if err != nil {
		printConfigError(err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTimeout := time.Duration(shutdownTimeoutSecs()) * time.Second

	// cleanup drains the primary and replica database pools after the HTTP
	// server has stopped accepting new connections. It always runs (even if
	// HTTP shutdown timed out) so half-open connections are closed before the
	// process exits. DrainPool is a no-op for the nil replica pool when no
	// replica is configured.
	cleanup := func(cleanupCtx context.Context) error {
		start := time.Now()
		defer func() {
			metrics.ShutdownDuration.Observe(time.Since(start).Seconds())
		}()
		if err := db.DrainPool(cleanupCtx, pool); err != nil {
			return err
		}
		return db.DrainPool(cleanupCtx, replicaPool)
	}

	log.Printf("server listening on %s", srv.Addr)
	if err := runHTTPServer(ctx, make(chan os.Signal, 1), srv, shutdownTimeout, cleanup); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// runHTTPServer starts srv and performs a graceful shutdown when ctx is
// cancelled.  The optional cleanup function is always called during shutdown
// (even if HTTP shutdown times out) so that resources such as the database
// pool are drained before the process exits.
// A second signal on secondSignal forces an immediate close.
func runHTTPServer(
	ctx context.Context,
	secondSignal <-chan os.Signal,
	srv *http.Server,
	shutdownTimeout time.Duration,
	cleanup func(context.Context) error,
) error {
	serverErr := make(chan error, 1)
	go func() {
		err := listenAndServe(srv)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serverErr <- err
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	// Always call cleanup on exit so resources (DB pool) are drained even
	// when the HTTP server shutdown times out.  cleanup gets a fresh context
	// to avoid racing with an expired shutdownCtx. Errors are logged rather
	// than returned so the HTTP shutdown error takes precedence.
	defer func() {
		if cleanup != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), shutdownTimeout)
			defer cleanupCancel()
			if err := cleanup(cleanupCtx); err != nil {
				log.Printf("cleanup error during shutdown: %v", err)
			}
		}
	}()

	// A second signal forces an immediate close.
	go func() {
		select {
		case <-secondSignal:
			srv.Close()
		case <-shutdownCtx.Done():
		}
	}()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("http server shutdown: %w", err)
	}

	if shutdownCtx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("http server shutdown: timed out")
	}

	if err := <-serverErr; err != nil {
		return err
	}

	return nil
}

func printConfigError(err error) {
	fmt.Fprintf(os.Stderr, "configuration error: %v\n", err)
}

// stdLogger adapts the standard log package to the worker's logger interface.
type stdLogger struct{}

func (stdLogger) Error(msg string, keysAndValues ...any) {
	log.Println(append([]any{"ERROR: " + msg}, keysAndValues...)...)
}

// shutdownTimeoutSecs reads the GRACEFUL_SHUTDOWN_TIMEOUT env var, falling
// back to 30 seconds. This is kept outside config.Load() so main() can
// consume the graceful shutdown timeout without threading the full Config
// through InitializeServer.
func shutdownTimeoutSecs() int {
	if v := os.Getenv("GRACEFUL_SHUTDOWN_TIMEOUT"); v != "" {
		if s, err := strconv.Atoi(v); err == nil && s >= 1 && s <= 600 {
			return s
		}
	}
	return 30
}
