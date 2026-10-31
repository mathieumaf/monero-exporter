package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	hangConnections = "get_connections"
	hangInfo        = "get_info"
	callPeerList    = "/get_peer_list"
)

//nolint:paralleltest // swaps the process-global Prometheus registerer.
func TestHangingEndpointDoesNotStarveLaterCollectors(t *testing.T) {
	body, calls := scrapeFakeDaemon(t, scrapeSpec{
		hang:           hangConnections,
		rpcTimeout:     500 * time.Millisecond,
		collectTimeout: 8 * time.Second,
		scrapeTimeout:  5 * time.Second,
	})

	connIdx := callIndex(calls, hangConnections)
	peerIdx := callIndex(calls, callPeerList)

	if connIdx < 0 || peerIdx < 0 || peerIdx < connIdx {
		t.Fatalf("peer list did not run after the hung call; calls=%v", calls)
	}

	if hasMetric(body, "monero_p2p_connections_age") {
		t.Fatal("hung connections collector emitted metrics")
	}

	if !hasMetric(body, "monero_peerlist") {
		t.Fatal("missing peer metrics from the collector after the hang")
	}

	assertGauge(t, body, "monero_info_height", 11)
	assertGauge(t, body, "monero_net_rx_bytes", 42)
	// get_info succeeded, so the node stays up. The hung connections
	// collector is reported on its own success series.
	assertGauge(t, body, "monero_up", 1)

	const connectionsFailed = `monero_exporter_collector_success{collector="connections"} 0`
	if !strings.Contains(body, connectionsFailed) {
		t.Fatalf("connections collector was not reported as failed:\n%s", body)
	}

	dur := gauge(t, body, "monero_exporter_collection_duration_seconds")
	if dur < 0.25 || dur >= 4 {
		t.Fatalf("collection duration = %v, want the rpc timeout to bound the hung call", dur)
	}
}

//nolint:paralleltest // swaps the process-global Prometheus registerer.
func TestCollectTimeoutAbortsBeforeLaterCollectors(t *testing.T) {
	body, calls := scrapeFakeDaemon(t, scrapeSpec{
		hang:           hangInfo,
		rpcTimeout:     10 * time.Second,
		collectTimeout: 500 * time.Millisecond,
		scrapeTimeout:  3 * time.Second,
	})

	if callIndex(calls, hangInfo) < 0 {
		t.Fatalf("get_info was not called; calls=%v", calls)
	}

	if callIndex(calls, callPeerList) >= 0 {
		t.Fatalf("later collector ran outside the collect budget; calls=%v", calls)
	}

	assertGauge(t, body, "monero_up", 0)

	dur := gauge(t, body, "monero_exporter_collection_duration_seconds")
	if dur >= 2 {
		t.Fatalf("collection duration = %v, want the collect timeout to abort the cycle", dur)
	}
}

type scrapeSpec struct {
	hang           string
	rpcTimeout     time.Duration
	collectTimeout time.Duration
	scrapeTimeout  time.Duration
}

func scrapeFakeDaemon(t *testing.T, spec scrapeSpec) (string, []string) {
	t.Helper()

	useTestRegistry(t)

	daemon := newFakeDaemon(spec.hang)
	srv := httptest.NewServer(daemon)
	t.Cleanup(srv.Close)

	addr := freeAddr(t)
	args := []string{
		"--monero-addr=" + srv.URL,
		"--bind-addr=" + addr,
		"--telemetry-path=/metrics",
		"--rpc-timeout=" + spec.rpcTimeout.String(),
		"--collect-timeout=" + spec.collectTimeout.String(),
		"--log.level=error",
		"--log.format=" + logFormatJSON,
	}
	startExporter(t, args)
	waitTCP(t, addr)

	body := getMetrics(t, addr, spec.scrapeTimeout)

	return body, daemon.snapshot()
}

func useTestRegistry(t *testing.T) {
	t.Helper()

	reg := prometheus.NewRegistry()
	prevRegisterer := prometheus.DefaultRegisterer
	prevGatherer := prometheus.DefaultGatherer
	prometheus.DefaultRegisterer = reg
	prometheus.DefaultGatherer = reg

	t.Cleanup(func() {
		prometheus.DefaultRegisterer = prevRegisterer
		prometheus.DefaultGatherer = prevGatherer
	})
}

func startExporter(t *testing.T, args []string) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	cmd := (&command{}).Cmd()
	cmd.SetContext(ctx)
	cmd.SetArgs(args)

	errCh := make(chan error, 1)

	go func() {
		errCh <- cmd.Execute()
	}()

	t.Cleanup(func() {
		cancel()

		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("exporter exit: %v", err)
			}
		case <-time.After(20 * time.Second):
			t.Error("exporter did not exit after cancel")
		}
	})
}

func getMetrics(t *testing.T, addr string, timeout time.Duration) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/metrics", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}

	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	return string(body)
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

func assertGauge(t *testing.T, body, name string, want float64) {
	t.Helper()

	got := gauge(t, body, name)
	if got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func gauge(t *testing.T, body, name string) float64 {
	t.Helper()

	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		metric := fields[0]
		if i := strings.IndexByte(metric, '{'); i >= 0 {
			metric = metric[:i]
		}

		if metric != name {
			continue
		}

		value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		return value
	}

	t.Fatalf("metric %s not found in:\n%s", name, body)

	return 0
}

func hasMetric(body, name string) bool {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		metric := strings.Fields(line)[0]
		if i := strings.IndexByte(metric, '{'); i >= 0 {
			metric = metric[:i]
		}

		if metric == name {
			return true
		}
	}

	return false
}

func callIndex(calls []string, name string) int {
	for i, call := range calls {
		if call == name {
			return i
		}
	}

	return -1
}

type fakeDaemon struct {
	mu        sync.Mutex
	hang      string
	calls     []string
	responses map[string][]byte
}

func newFakeDaemon(hang string) *fakeDaemon {
	return &fakeDaemon{
		hang:      hang,
		responses: daemonResponses(),
	}
}

func (f *fakeDaemon) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() { _ = r.Body.Close() }()

	name := f.callName(r)
	f.record(name)

	if name == f.hang {
		<-r.Context().Done()

		return
	}

	payload, ok := f.responses[name]
	if !ok {
		http.Error(w, "unexpected "+name, http.StatusNotFound)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(payload)
}

func (f *fakeDaemon) callName(r *http.Request) string {
	if r.URL.Path != "/json_rpc" {
		return r.URL.Path
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "unreadable"
	}

	var req struct {
		Method string `json:"method"`
	}

	if err := json.Unmarshal(body, &req); err != nil || req.Method == "" {
		return "bad-jsonrpc"
	}

	return req.Method
}

func (f *fakeDaemon) record(name string) {
	f.mu.Lock()
	f.calls = append(f.calls, name)
	f.mu.Unlock()
}

func (f *fakeDaemon) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]string, len(f.calls))
	copy(out, f.calls)

	return out
}

func daemonResponses() map[string][]byte {
	blockHeader := map[string]any{"height": 10, "hash": "ab"}

	return map[string][]byte{
		hangInfo: mustJSON(jsonRPC(map[string]any{
			"height": 11, "mainnet": true, "start_time": 1,
		})),
		"get_last_block_header": mustJSON(jsonRPC(map[string]any{
			"block_header": blockHeader,
		})),
		"get_block": mustJSON(jsonRPC(map[string]any{
			"json":         `{"tx_hashes":[]}`,
			"block_header": blockHeader,
		})),
		"rpc_access_tracking": mustJSON(jsonRPC(map[string]any{
			"data": []any{map[string]any{
				"count": 1, "rpc": hangInfo, "time": 1,
			}},
		})),
		hangConnections: mustJSON(jsonRPC(map[string]any{
			"connections": []any{},
		})),
		"/get_transaction_pool_stats": mustJSON(okBody(map[string]any{
			"pool_stats": map[string]any{},
		})),
		"/get_transaction_pool": mustJSON(okBody(map[string]any{
			"transactions":     []any{},
			"spent_key_images": []any{},
		})),
		"/get_net_stats": mustJSON(okBody(map[string]any{
			"total_bytes_in": 42, "total_bytes_out": 7,
		})),
		"/get_transactions": mustJSON(okBody(map[string]any{
			"txs": []any{},
		})),
		callPeerList: mustJSON(okBody(map[string]any{
			"white_list": []any{},
			"gray_list":  []any{},
		})),
	}
}

func okBody(body map[string]any) map[string]any {
	body["status"] = "OK"

	return body
}

func jsonRPC(result any) map[string]any {
	return map[string]any{
		"id":      "0",
		"jsonrpc": "2.0",
		"result":  result,
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	return b
}
