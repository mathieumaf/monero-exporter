package collector_test

import (
	"math"
	"testing"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

func TestNetTxBytesHelpDescribesSentBytes(t *testing.T) {
	t.Parallel()

	families := gather(t, newCollector(newFixtureDaemon()))
	family := familyByName(families, "monero_net_tx_bytes")
	if family == nil {
		t.Fatal("missing monero_net_tx_bytes")
	}

	const want = "number of bytes sent by this node"
	if family.GetHelp() != want {
		t.Fatalf("help %q, want %q", family.GetHelp(), want)
	}
}

func TestConnectionRatesSkipZeroLiveTime(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	f.setJSON("get_connections", map[string]any{
		jsonConnections: []any{
			connectionFixture(true, "before_handshake", 0, 50, 50),
			connectionFixture(false, "normal", 4, 100, 40),
		},
	})

	families := gather(t, newCollector(f))
	assertFiniteMetrics(t, families)

	family := familyByName(families, "monero_p2p_connections_rx_rate_bps")
	summary := family.GetMetric()[0].GetSummary()
	if summary.GetSampleCount() != 1 || summary.GetSampleSum() != 25 {
		t.Fatalf("rx rate count=%d sum=%v, want count 1 sum 25",
			summary.GetSampleCount(), summary.GetSampleSum())
	}
}

func TestOnlyZeroLiveTimeExportsFiniteRates(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	f.setJSON("get_connections", map[string]any{
		jsonConnections: []any{
			connectionFixture(true, "normal", 0, 0, 10),
		},
	})

	families := gather(t, newCollector(f))
	assertFiniteMetrics(t, families)

	family := familyByName(families, "monero_p2p_connections_tx_rate_bps")
	if family.GetMetric()[0].GetSummary().GetSampleCount() != 0 {
		t.Fatal("zero live_time connection was included in the rate summary")
	}
}

func TestFailingCollectorKeepsLastSeries(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	c := newCollector(f, collector.WithLogger(logr.Discard()))
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	first := mustGather(t, reg)
	assertGauge(t, first, "monero_net_tx_bytes", nil, 222)
	assertGauge(t, first, "monero_info_height", nil, fixtureHeight)

	f.failRaw("/get_net_stats", errFake)
	f.setJSON("get_info", map[string]any{"height": 50, "top_block_hash": fixtureTop})

	second := mustGather(t, reg)
	assertGauge(t, second, "monero_net_tx_bytes", nil, 222)
	assertGauge(t, second, "monero_net_rx_bytes", nil, 111)
	assertGauge(t, second, "monero_info_height", nil, 50)
	assertGauge(t, second, "monero_peerlist", map[string]string{labelType: "white"}, 2)
}

func connectionFixture(incoming bool, state string, live, recv, send uint64) map[string]any {
	return map[string]any{
		"incoming":   incoming,
		"state":      state,
		"live_time":  live,
		"recv_count": recv,
		"send_count": send,
		jsonHeight:   fixtureHeight,
	}
}

func mustGather(t *testing.T, reg prometheus.Gatherer) []*dto.MetricFamily {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	return families
}

func assertFiniteMetrics(t *testing.T, families []*dto.MetricFamily) {
	t.Helper()

	for _, family := range families {
		for _, metric := range family.GetMetric() {
			assertFiniteValue(t, family.GetName(), metric)
		}
	}
}

func assertFiniteValue(t *testing.T, name string, metric *dto.Metric) {
	t.Helper()

	check := func(v float64) {
		t.Helper()

		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("%s exported non-finite value %v", name, v)
		}
	}

	if gauge := metric.GetGauge(); gauge != nil {
		check(gauge.GetValue())
	}

	summary := metric.GetSummary()
	if summary == nil {
		return
	}

	check(summary.GetSampleSum())
	for _, quantile := range summary.GetQuantile() {
		check(quantile.GetValue())
	}
}
