package collector_test

import (
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

func TestDisabledNetCollectorSkipsRPC(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	c := newCollector(f, collector.WithCollectorEnabled(collector.CollectorNet, false))
	families := gather(t, c)

	if familyByName(families, "monero_net_rx_bytes") != nil {
		t.Fatal("disabled net collector still exported bytes")
	}

	if f.rawCallsFor("/get_net_stats") != 0 {
		t.Fatalf("get_net_stats calls = %d, want 0", f.rawCallsFor("/get_net_stats"))
	}

	assertGauge(t, families, "monero_info_height", nil, fixtureHeight)
}

func TestPeerlistDisabledKeepsCountsFromGetInfo(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	f.setJSON("get_info", map[string]any{
		jsonHeight:     fixtureHeight,
		jsonTopHash:    fixtureTop,
		jsonWhitePeers: 0,
		jsonGreyPeers:  7,
	})

	c := newCollector(f, collector.WithCollectorEnabled(collector.CollectorPeerlist, false))
	families := gather(t, c)

	assertGauge(t, families, "monero_peerlist", map[string]string{labelType: labelWhite}, 0)
	assertGauge(t, families, "monero_peerlist", map[string]string{labelType: "gray"}, 7)
	if familyByName(families, "monero_peerlist_lastseen") != nil {
		t.Fatal("disabled peerlist collector still exported lastseen")
	}

	if f.rawCallsFor("/get_peer_list") != 0 {
		t.Fatalf("get_peer_list calls = %d, want 0", f.rawCallsFor("/get_peer_list"))
	}
}

func TestConnectionCountsComeFromGetInfoIncludingZeros(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	f.setJSON("get_info", map[string]any{
		jsonHeight:                   fixtureHeight,
		jsonTopHash:                  fixtureTop,
		"incoming_connections_count": 0,
		"outgoing_connections_count": 5,
		jsonWhitePeers:               2,
		jsonGreyPeers:                1,
	})
	f.setJSON("get_connections", map[string]any{
		jsonConnections: []any{
			connectionFixture(false, "normal", 4, 10, 10),
			connectionFixture(false, "normal", 4, 10, 10),
		},
	})

	families := gather(t, newCollector(f))
	family := familyByName(families, "monero_p2p_connections")
	if family == nil {
		t.Fatal("missing monero_p2p_connections")
	}

	if got := len(family.GetMetric()); got != 2 {
		t.Fatalf("connection series = %d, want 2", got)
	}

	assertGauge(t, families, "monero_p2p_connections", map[string]string{
		labelType: "in", labelState: labelTotal,
	}, 0)
	assertGauge(t, families, "monero_p2p_connections", map[string]string{
		labelType: labelOut, labelState: labelTotal,
	}, 5)
}

func TestConnectionsDisabledStillExportsCounts(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	c := newCollector(f, collector.WithCollectorEnabled(collector.CollectorConnections, false))
	families := gather(t, c)

	assertGauge(t, families, "monero_p2p_connections", map[string]string{
		labelType: "in", labelState: labelTotal,
	}, 1)
	assertGauge(t, families, "monero_p2p_connections", map[string]string{
		labelType: labelOut, labelState: labelTotal,
	}, 0)
	if familyByName(families, "monero_p2p_connections_age") != nil {
		t.Fatal("disabled connections collector still exported ages")
	}

	if f.jsonCallsFor("get_connections") != 0 {
		t.Fatalf("get_connections calls = %d, want 0", f.jsonCallsFor("get_connections"))
	}
}

func TestPeerlistIntervalServesCache(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	now := time.Unix(1_700_000_000, 0)
	c := newCollector(f,
		collector.WithLogger(logr.Discard()),
		collector.WithNow(func() time.Time { return now }),
		collector.WithCollectorInterval(collector.CollectorPeerlist, time.Hour),
	)
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	if _, err := reg.Gather(); err != nil {
		t.Fatalf("first gather: %v", err)
	}

	if f.rawCallsFor("/get_peer_list") != 1 {
		t.Fatalf("first cycle get_peer_list calls = %d, want 1", f.rawCallsFor("/get_peer_list"))
	}

	now = now.Add(time.Minute)
	families := mustGather(t, reg)
	if f.rawCallsFor("/get_peer_list") != 1 {
		t.Fatalf("cached cycle get_peer_list calls = %d, want 1", f.rawCallsFor("/get_peer_list"))
	}

	if familyByName(families, "monero_peerlist_lastseen") == nil {
		t.Fatal("interval cache dropped monero_peerlist_lastseen")
	}

	now = now.Add(time.Hour)
	if _, err := reg.Gather(); err != nil {
		t.Fatalf("third gather: %v", err)
	}

	if f.rawCallsFor("/get_peer_list") != 2 {
		t.Fatalf("after interval get_peer_list calls = %d, want 2", f.rawCallsFor("/get_peer_list"))
	}
}

func TestOverallDisabledSkipsGetInfo(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	c := newCollector(f,
		collector.WithLogger(logr.Discard()),
		collector.WithCollectorEnabled(collector.CollectorOverall, false),
	)
	families := gather(t, c)
	if f.jsonCallsFor("get_info") != 0 {
		t.Fatalf("get_info calls = %d, want 0", f.jsonCallsFor("get_info"))
	}

	if familyByName(families, "monero_info_height") != nil {
		t.Fatal("disabled overall collector still exported height")
	}

	if familyByName(families, "monero_peerlist") != nil {
		t.Fatal("peer counts were exported without get_info")
	}

	assertGauge(t, families, "monero_up", nil, 0)
}
