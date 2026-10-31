package collector_test

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

func TestFailingCollectorKeepsPeersAndMoneroUp(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	c := newCollector(f, collector.WithLogger(logr.Discard()))
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	first := mustGather(t, reg)
	assertGauge(t, first, "monero_up", nil, 1)
	assertCollectorSuccess(t, first, collector.CollectorNet, 1)
	assertGauge(t, first, "monero_net_tx_bytes", nil, 222)

	netSuccess := gaugeOrFatal(t, first, "monero_exporter_last_success_timestamp_seconds", map[string]string{
		labelCollector: collector.CollectorNet,
	})
	if netSuccess <= 0 {
		t.Fatalf("net last success timestamp = %v, want > 0", netSuccess)
	}

	f.failRaw("/get_net_stats", errFake)
	second := mustGather(t, reg)

	assertGauge(t, second, "monero_up", nil, 1)
	assertGauge(t, second, "monero_info_height", nil, fixtureHeight)
	assertGauge(t, second, "monero_net_tx_bytes", nil, 222)
	assertGauge(t, second, "monero_net_rx_bytes", nil, 111)
	assertCollectorSuccess(t, second, collector.CollectorNet, 0)
	assertCollectorSuccess(t, second, collector.CollectorOverall, 1)
	assertGauge(t, second, "monero_peerlist", map[string]string{labelType: labelWhite}, 2)

	again := gaugeOrFatal(t, second, "monero_exporter_last_success_timestamp_seconds", map[string]string{
		labelCollector: collector.CollectorNet,
	})
	if again != netSuccess {
		t.Fatalf("net last success moved from %v to %v after a failure", netSuccess, again)
	}
}

func TestGetInfoFailureClearsMoneroUpOnly(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	f.failJSON("get_info", errFake)
	families := gather(t, newCollector(f, collector.WithLogger(logr.Discard())))

	assertGauge(t, families, "monero_up", nil, 0)
	assertCollectorSuccess(t, families, collector.CollectorOverall, 0)
	assertCollectorSuccess(t, families, collector.CollectorNet, 1)
	assertGauge(t, families, "monero_net_rx_bytes", nil, 111)
	if familyByName(families, "monero_info_height") != nil {
		t.Fatal("get_info failed but height was exported")
	}
}

func TestDisabledCollectorOmitsHealthSeries(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	families := gather(t, newCollector(f,
		collector.WithLogger(logr.Discard()),
		collector.WithCollectorEnabled(collector.CollectorNet, false),
	))

	_, ok := gaugeValue(familyByName(families, "monero_exporter_collector_success"), map[string]string{
		labelCollector: collector.CollectorNet,
	})
	if ok {
		t.Fatal("disabled net collector still exported a success series")
	}

	assertCollectorSuccess(t, families, collector.CollectorOverall, 1)
	assertGauge(t, families, "monero_up", nil, 1)
}

func assertCollectorSuccess(t *testing.T, families []*dto.MetricFamily, name string, want float64) {
	t.Helper()

	assertGauge(t, families, "monero_exporter_collector_success", map[string]string{
		labelCollector: name,
	}, want)
}

func gaugeOrFatal(
	t *testing.T, families []*dto.MetricFamily, name string, labels map[string]string,
) float64 {
	t.Helper()

	got, ok := gaugeValue(familyByName(families, name), labels)
	if !ok {
		t.Fatalf("metric %s %v not found", name, labels)
	}

	return got
}
