package exporter_test

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/mathieumaf/monero-exporter/pkg/exporter"
)

func TestRunShutsDownOnCancel(t *testing.T) {
	t.Parallel()

	addr := freeAddr(t)

	srv, err := exporter.New(
		exporter.WithBindAddress(addr),
		exporter.WithTelemetryPath("/metrics"),
		exporter.WithLogger(logr.Discard()),
	)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.Run(ctx)
	}()

	waitTCP(t, addr)

	status := getStatus(t, "http://"+addr+"/metrics")
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not return")
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestRunListenError(t *testing.T) {
	t.Parallel()

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	srv, err := exporter.New(
		exporter.WithBindAddress(ln.Addr().String()),
		exporter.WithLogger(logr.Discard()),
	)
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}

	if err := srv.Run(context.Background()); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestNewDefaults(t *testing.T) {
	t.Parallel()

	srv, err := exporter.New()
	if err != nil {
		t.Fatalf("new exporter: %v", err)
	}

	if srv == nil {
		t.Fatal("nil exporter")
	}
}

func getStatus(t *testing.T, url string) int {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close body: %v", err)
		}
	}()

	return resp.StatusCode
}

func waitTCP(t *testing.T, addr string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	dialer := &net.Dialer{Timeout: 100 * time.Millisecond}

	for {
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			if err := conn.Close(); err != nil {
				t.Fatalf("close probe: %v", err)
			}

			return
		}

		if ctx.Err() != nil {
			t.Fatalf("timeout waiting for %s: %v", addr, err)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

func freeAddr(t *testing.T) string {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	addr := ln.Addr().String()

	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	return addr
}
