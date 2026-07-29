package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// listenAndServe is a package-level variable so tests can inject a fake
// listener without network access.
var listenAndServe = func(srv *http.Server) error {
	return srv.ListenAndServe()
}

func main() {
	srv, err := InitializeServer()
	if err != nil {
		printConfigError(err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("server listening on %s", srv.Addr)
	if err := runHTTPServer(ctx, make(chan os.Signal, 1), srv, 30*time.Second, nil); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

// runHTTPServer starts srv and performs a graceful shutdown when ctx is
// cancelled.  The optional cleanup function is called during shutdown.
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

	if cleanup != nil {
		if err := cleanup(shutdownCtx); err != nil {
			return fmt.Errorf("cleanup: %w", err)
		}
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
