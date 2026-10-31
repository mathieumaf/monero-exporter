package collector_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

// errFake is returned by the stub daemon for injected failures and for calls
// the fixture does not know about.
var errFake = errors.New("fake daemon")

// fakeDaemon is a daemon.Requester that serves canned JSON and records calls.
type fakeDaemon struct {
	mu sync.Mutex

	json    map[string][]byte
	raw     map[string][]byte
	jsonErr map[string]error
	rawErr  map[string]error

	jsonCalls []string
	rawCalls  []string
}

func newFakeDaemon() *fakeDaemon {
	return &fakeDaemon{
		json:    map[string][]byte{},
		raw:     map[string][]byte{},
		jsonErr: map[string]error{},
		rawErr:  map[string]error{},
	}
}

func (f *fakeDaemon) setJSON(method string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	f.mu.Lock()
	f.json[method] = body
	f.mu.Unlock()
}

func (f *fakeDaemon) failRaw(endpoint string, err error) {
	f.mu.Lock()
	f.rawErr[endpoint] = err
	f.mu.Unlock()
}

func (f *fakeDaemon) setRaw(endpoint string, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}

	f.mu.Lock()
	f.raw[endpoint] = body
	f.mu.Unlock()
}

func (f *fakeDaemon) JSONRPC(
	_ context.Context, method string, _ any, result any,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.jsonCalls = append(f.jsonCalls, method)

	if err := f.jsonErr[method]; err != nil {
		return err
	}

	body, ok := f.json[method]
	if !ok {
		return fmt.Errorf("unexpected jsonrpc %s: %w", method, errFake)
	}

	if err := json.Unmarshal(body, result); err != nil {
		return fmt.Errorf("unmarshal %s: %w", method, err)
	}

	return nil
}

func (f *fakeDaemon) RawRequest(
	_ context.Context, endpoint string, _ any, response any,
) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rawCalls = append(f.rawCalls, endpoint)

	if err := f.rawErr[endpoint]; err != nil {
		return err
	}

	body, ok := f.raw[endpoint]
	if !ok {
		return fmt.Errorf("unexpected raw %s: %w", endpoint, errFake)
	}

	if err := json.Unmarshal(body, response); err != nil {
		return fmt.Errorf("unmarshal %s: %w", endpoint, err)
	}

	return nil
}

func (f *fakeDaemon) client() *daemon.Client {
	return daemon.NewClient(f)
}

const (
	fixtureHeight = 12345
	fixtureTop    = "abcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcabcd"
	fixtureTxHash = "tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1tx1"

	jsonHeight      = "height"
	jsonConnections = "connections"
)

// newFixtureDaemon returns a daemon whose responses let every collector emit
// at least one sample of each series it owns.
func newFixtureDaemon() *fakeDaemon {
	txJSON := fixtureTxJSON()

	f := newFakeDaemon()
	f.setChainFixtures()
	f.setNetworkFixtures()
	f.setPoolFixtures(txJSON)
	f.setRaw("/get_transactions", map[string]any{
		"txs": []any{
			map[string]any{
				"tx_hash": fixtureTxHash,
				"as_hex":  "abcd",
				"as_json": string(txJSON),
			},
		},
	})

	return f
}

func fixtureTxJSON() []byte {
	return []byte(`{
		"version": 2,
		"vin": [{"key": {"amount": 0}}],
		"vout": [
			{"amount": 1, "target": {"key": "aa"}},
			{"amount": 2, "target": {"key": "bb"}}
		],
		"rct_signatures": {"txnFee": 1000000}
	}`)
}

func (f *fakeDaemon) setChainFixtures() {
	f.setJSON("get_info", map[string]any{
		jsonHeight:                   fixtureHeight,
		"target_height":              0,
		"top_block_hash":             fixtureTop,
		"incoming_connections_count": 1,
		"outgoing_connections_count": 0,
		"white_peerlist_size":        2,
		"grey_peerlist_size":         1,
		"mainnet":                    true,
		"synchronized":               true,
		"start_time":                 1_700_000_000,
		"alt_blocks_count":           3,
		"block_size_limit":           300000,
		"block_size_median":          100000,
		"rpc_connections_count":      2,
		"database_size":              4096,
		"free_space":                 8192,
	})
	header := map[string]any{"hash": fixtureTop, jsonHeight: fixtureHeight}
	f.setJSON("get_last_block_header", map[string]any{"block_header": header})
	f.setJSON("get_block", map[string]any{
		"block_header": map[string]any{
			"hash":       fixtureTop,
			jsonHeight:   fixtureHeight,
			"difficulty": 99,
			"reward":     700_000_000_000,
			"block_size": 300,
			"num_txes":   1,
		},
		"json": `{"tx_hashes":["` + fixtureTxHash + `"]}`,
	})
}

func (f *fakeDaemon) setNetworkFixtures() {
	f.setJSON("get_connections", map[string]any{
		jsonConnections: []any{
			map[string]any{
				"incoming":   true,
				"state":      "normal",
				"live_time":  10,
				"recv_count": 100,
				"send_count": 40,
				jsonHeight:   fixtureHeight,
			},
		},
	})
	f.setJSON("rpc_access_tracking", map[string]any{
		"data": []any{
			map[string]any{"rpc": "get_info", "count": 4, "time": 1_000_000_000},
		},
	})
	f.setRaw("/get_peer_list", map[string]any{
		"white_list": []any{
			peerEntry("10.0.0.1", 1_700_000_000),
			peerEntry("10.0.0.2", 1_700_000_050),
		},
		"gray_list": []any{
			peerEntry("10.0.0.3", 1_700_000_080),
		},
	})
	f.setRaw("/get_net_stats", map[string]any{
		"total_bytes_in":  111,
		"total_bytes_out": 222,
	})
}

func peerEntry(host string, lastSeen int) map[string]any {
	return map[string]any{"host": host, "last_seen": lastSeen}
}

func (f *fakeDaemon) setPoolFixtures(txJSON []byte) {
	f.setRaw("/get_transaction_pool_stats", map[string]any{
		"pool_stats": map[string]any{
			"bytes_total":       2048,
			"fee_total":         2_000_000_000_000,
			"txs_total":         1,
			"num_failing":       0,
			"num_double_spends": 0,
			"num_not_relayed":   1,
			"num_10m":           0,
		},
	})
	f.setRaw("/get_transaction_pool", map[string]any{
		"spent_key_images": []any{
			map[string]any{"id_hash": "ki", "txs_hashes": []any{fixtureTxHash}},
		},
		"transactions": []any{
			map[string]any{
				"blob_size":    1024,
				"fee":          1_000_000,
				"receive_time": 1_700_000_100,
				"tx_json":      string(txJSON),
			},
		},
	})
}

func newCollector(f *fakeDaemon, opts ...collector.Option) *collector.Collector {
	c, err := collector.New(f.client(), opts...)
	if err != nil {
		panic(err)
	}

	return c
}

func gather(t testingT, c prometheus.Collector) []*dto.MetricFamily {
	t.Helper()

	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	return families
}

// testingT is the subset of *testing.T used by helpers so they stay small.
type testingT interface {
	Helper()
	Fatalf(format string, args ...any)
}

func familyByName(families []*dto.MetricFamily, name string) *dto.MetricFamily {
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}

	return nil
}

func gaugeValue(family *dto.MetricFamily, labels map[string]string) (float64, bool) {
	if family == nil {
		return 0, false
	}

	for _, metric := range family.GetMetric() {
		if !labelsMatch(metric, labels) {
			continue
		}

		if metric.GetGauge() == nil {
			return 0, false
		}

		return metric.GetGauge().GetValue(), true
	}

	return 0, false
}

func labelsMatch(metric *dto.Metric, want map[string]string) bool {
	if len(metric.GetLabel()) != len(want) {
		return false
	}

	for _, label := range metric.GetLabel() {
		value, ok := want[label.GetName()]
		if !ok || value != label.GetValue() {
			return false
		}
	}

	return true
}
