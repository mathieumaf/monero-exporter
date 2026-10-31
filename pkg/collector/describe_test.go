package collector_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// describedMetrics is the number of descriptors Describe must publish. Bump it
// when a new series is added; pedantic registration fails if a collected
// series is missing from this set.
const (
	describedMetrics = 52

	labelType  = "type"
	labelState = "state"
)

func TestDescribeRegistersCheckedCollector(t *testing.T) {
	t.Parallel()

	c := newCollector(newFixtureDaemon())

	ch := make(chan *prometheus.Desc, describedMetrics+8)
	c.Describe(ch)
	close(ch)

	n := 0
	for range ch {
		n++
	}

	if n != describedMetrics {
		t.Fatalf("Describe sent %d descriptors, want %d", n, describedMetrics)
	}

	families := gather(t, c)
	assertGauge(t, families, "monero_up", nil, 1)
	assertGauge(t, families, "monero_info_height", nil, 12345)
	assertGauge(t, families, "monero_net_rx_bytes", nil, 111)
	assertGauge(t, families, "monero_net_tx_bytes", nil, 222)
	assertGauge(t, families, "monero_peerlist", map[string]string{labelType: "white"}, 2)
	assertGauge(t, families, "monero_peerlist", map[string]string{labelType: "gray"}, 1)
	assertGauge(t, families, "monero_p2p_connections", map[string]string{
		labelType:  "in",
		labelState: "normal",
	}, 1)
	assertGauge(t, families, "monero_rpc_hits_total", map[string]string{
		"method": "get_info",
	}, 4)
	assertGauge(t, families, "monero_lastblock_height", nil, 12345)
	assertGauge(t, families, "monero_transaction_pool_transactions", nil, 1)

	if familyByName(families, "monero_exporter_collection_duration_seconds") == nil {
		t.Fatal("missing monero_exporter_collection_duration_seconds")
	}
}

func assertGauge(
	t *testing.T,
	families []*dto.MetricFamily,
	name string,
	labels map[string]string,
	want float64,
) {
	t.Helper()

	family := familyByName(families, name)
	got, ok := gaugeValue(family, labels)
	if !ok {
		t.Fatalf("metric %s %v not found", name, labels)
	}

	if got != want {
		t.Fatalf("metric %s %v = %v, want %v", name, labels, got, want)
	}
}
