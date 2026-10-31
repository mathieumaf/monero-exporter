package exporter

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

const (
	defaultBindAddress   = ":9000"
	defaultTelemetryPath = "/metrics"

	// readHeaderTimeout bounds how long the metrics server waits for a
	// client to send its request headers, guarding against slow-loris
	// style connections.
	readHeaderTimeout = 10 * time.Second

	// defaultShutdownTimeout bounds http.Server.Shutdown after the process
	// context is cancelled (SIGINT/SIGTERM). It stays under a typical
	// container termination grace period. When it expires the listener is
	// closed and Run still returns nil so the process can exit 0.
	defaultShutdownTimeout = 15 * time.Second
)

// Exporter is responsible for bringing up a web server that collects metrics
// that have been globally registered via prometheus collectors (e.g., see
// `pkg/collector`).
type Exporter struct {
	bindAddress     string
	telemetryPath   string
	shutdownTimeout time.Duration

	server *http.Server
	log    logr.Logger
}

// WithTelemetryPath overrides the default path under which the prometheus
// metrics are reported.
//
// For instance:
//   - /
//   - /metrics
//   - /telemetry
func WithTelemetryPath(v string) Option {
	return func(e *Exporter) {
		e.telemetryPath = v
	}
}

// WithBindAddress overrides the default address at which the prometheus
// metrics HTTP server would bind to.
//
// Examples:
//   - :8080
//   - 127.0.0.2:1313
func WithBindAddress(v string) Option {
	return func(e *Exporter) {
		e.bindAddress = v
	}
}

// WithLogger overrides the exporter logger. When unset, New builds a
// production zap logger at info level.
func WithLogger(log logr.Logger) Option {
	return func(e *Exporter) {
		e.log = log
	}
}

// Option allows overriding the exporter's defaults.
type Option func(e *Exporter)

// New instantiates a new exporter with defaults, unless options are passed.
func New(opts ...Option) (*Exporter, error) {
	e := &Exporter{
		bindAddress:     defaultBindAddress,
		telemetryPath:   defaultTelemetryPath,
		shutdownTimeout: defaultShutdownTimeout,
	}

	for _, opt := range opts {
		opt(e)
	}

	if e.log.GetSink() == nil {
		defaultLogger, err := zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("zap new production: %w", err)
		}

		e.log = zapr.NewLogger(defaultLogger.Named("exporter"))
	}

	return e, nil
}

// Run initiates the HTTP server to serve the metrics and blocks until ctx is
// cancelled or the server fails.
//
// Cancellation (SIGINT/SIGTERM via the process context) stops the server with
// http.Server.Shutdown, bounded by shutdownTimeout, and returns nil so the
// caller can exit 0. A listen or serve failure is returned.
//
// ps.: this is a BLOCKING method - make sure you either make use of goroutines
// to not block if needed.
func (e *Exporter) Run(ctx context.Context) error {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", e.bindAddress)
	if err != nil {
		return fmt.Errorf("listen on '%s': %w", e.bindAddress, err)
	}

	mux := http.NewServeMux()
	mux.Handle(e.telemetryPath, promhttp.Handler())

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	e.server = srv

	errCh := make(chan error, 1)

	go func() {
		serveErr := srv.Serve(ln)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- fmt.Errorf("serve on %s: %w", e.bindAddress, serveErr)

			return
		}

		errCh <- nil
	}()

	e.log.WithValues(
		"addr", e.bindAddress,
		"path", e.telemetryPath,
	).Info("listening")

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("metrics server: %w", err)
		}

		return nil
	case <-ctx.Done():
		e.shutdown()

		return nil
	}
}

// shutdown waits for in-flight scrapes, then force-closes if they outlive the
// budget. The process still exits successfully: a stuck scrape must not turn
// SIGTERM into a failure.
func (e *Exporter) shutdown() {
	if e.server == nil {
		return
	}

	e.log.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), e.shutdownTimeout)
	defer cancel()

	if err := e.server.Shutdown(shutdownCtx); err != nil {
		e.log.Error(err, "graceful shutdown failed; closing")
		_ = e.server.Close()
	}
}

// Close closes the metrics server. It is safe to call after Run has already
// shut the server down.
func (e *Exporter) Close() error {
	if e.server == nil {
		return nil
	}

	e.log.Info("closing")

	if err := e.server.Close(); err != nil && !isAlreadyClosed(err) {
		return fmt.Errorf("close: %w", err)
	}

	return nil
}

func isAlreadyClosed(err error) bool {
	return errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed)
}
