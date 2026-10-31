package collector_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

func TestCollectorValues(t *testing.T) {
	t.Parallel()

	srv := newFakeMonerod(t)
	client := srv.client(t, "", "")

	t.Run("overall", func(t *testing.T) {
		t.Parallel()
		assertOverall(t, client)
	})

	t.Run("last block", func(t *testing.T) {
		t.Parallel()
		assertLastBlock(t, client)
	})

	t.Run("transaction pool", func(t *testing.T) {
		t.Parallel()
		assertPoolMetrics(t, client)
	})

	t.Run("rpc", func(t *testing.T) {
		t.Parallel()
		assertRPC(t, client)
	})

	t.Run("net", func(t *testing.T) {
		t.Parallel()
		assertNet(t, client)
	})

	t.Run("connections", func(t *testing.T) {
		t.Parallel()
		assertConnections(t, client)
	})

	t.Run("peers", func(t *testing.T) {
		t.Parallel()
		assertPeers(t, client)
	})
}

func assertOverall(t *testing.T, client *daemon.Client) {
	t.Helper()

	text := collectCustom(t, func(ch chan<- prometheus.Metric) collector.CustomCollector {
		return collector.NewOverallCollector(client, ch)
	})

	near(t, gauge(t, text, "monero_info_uptime_seconds_total"), 86400)
	near(t, gauge(t, text, "monero_info_alternative_blocks"), 4)
	near(t, gauge(t, text, "monero_info_offline"), 0)
	near(t, gauge(t, text, "monero_info_mainnet"), 1)
	near(t, gauge(t, text, "monero_info_block_size_limit_bytes"), 600000)
	near(t, gauge(t, text, "monero_info_block_size_median_bytes"), 300000)
	near(t, gauge(t, text, "monero_info_synchronized"), 1)
	near(t, gauge(t, text, "monero_info_height"), 3500001)
	near(t, gauge(t, text, "monero_info_target_height"), 0)
	near(t, gauge(t, text, "monero_info_sync_ratio"), 1)
	near(t, gauge(t, text, "monero_info_rpc_connections"), 3)
	near(t, gauge(t, text, "monero_info_database_size_bytes"), 220000000000)
	near(t, gauge(t, text, "monero_info_free_space_bytes"), 800000000000)
}

func assertLastBlock(t *testing.T, client *daemon.Client) {
	t.Helper()

	text := collectCustom(t, func(ch chan<- prometheus.Metric) collector.CustomCollector {
		return collector.NewLastBlockStatsCollector(client, ch)
	})

	near(t, gauge(t, text, "monero_lastblock_size_bytes"), 18432)
	near(t, gauge(t, text, "monero_lastblock_difficulty"), 320000000000)
	near(t, gauge(t, text, "monero_lastblock_height"), 3500000)
	near(t, gauge(t, text, "monero_lastblock_transactions"), 2)
	near(t, gauge(t, text, "monero_lastblock_fees_monero"), 0.028)
	near(t, gauge(t, text, "monero_lastblock_reward_monero"), 0.628)
	near(t, gauge(t, text, "monero_lastblock_subsidy_monero"), 0.6)

	requireSummary(t, text, "monero_lastblock_transactions_size_bytes", 2, 250)
	requireSummary(t, text, "monero_lastblock_transactions_inputs", 2, 3)
	requireSummary(t, text, "monero_lastblock_transactions_outputs", 2, 3)
	requireSummary(t, text, "monero_lastblock_fees_micronero_per_kb", 2, feePerKBSum())
}

func assertPoolMetrics(t *testing.T, client *daemon.Client) {
	t.Helper()

	text := collectCustom(t, func(ch chan<- prometheus.Metric) collector.CustomCollector {
		return collector.NewTransactionPoolCollector(client, ch)
	})

	near(t, gauge(t, text, "monero_transaction_pool_spent_key_images"), 2)
	near(t, gauge(t, text, "monero_transaction_pool_transactions"), 2)
	near(t, gauge(t, text, "monero_transaction_pool_size_bytes"), 3072)
	near(t, gauge(t, text, "monero_transaction_pool_fees_monero"), 0.014)
	near(t, gauge(t, text, "monero_transaction_pool_failing_transactions"), 1)
	near(t, gauge(t, text, "monero_transaction_pool_double_spends"), 2)
	near(t, gauge(t, text, "monero_transaction_pool_not_relayed"), 3)
	near(t, gauge(t, text, "monero_transaction_pool_older_than_10m"), 4)

	requireSummary(t, text, "monero_transaction_pool_transactions_size_bytes", 2, 3072)
	requireSummary(t, text, "monero_transaction_pool_transactions_inputs", 2, 3)
	requireSummary(t, text, "monero_transaction_pool_transactions_outputs", 2, 5)
	requireSummary(t, text, "monero_transaction_pool_transactions_age", 2, 720)
	requireSummary(t, text, "monero_transaction_pool_fees_micronero_per_kb", 2, 12000)
}

func assertRPC(t *testing.T, client *daemon.Client) {
	t.Helper()

	text := collectCustom(t, func(ch chan<- prometheus.Metric) collector.CustomCollector {
		return collector.NewRPCCollector(client, ch)
	})

	near(t, gaugeWith(t, text, "monero_rpc_hits_total", `method="get_info"`), 12)
	near(t, gaugeWith(t, text, "monero_rpc_seconds_total", `method="get_info"`), 1.5)
	near(t, gaugeWith(t, text, "monero_rpc_hits_total", `method="/get_transaction_pool"`), 3)
	near(t, gaugeWith(t, text, "monero_rpc_seconds_total", `method="/get_transaction_pool"`), 0.25)
}

func assertNet(t *testing.T, client *daemon.Client) {
	t.Helper()

	text := collectCustom(t, func(ch chan<- prometheus.Metric) collector.CustomCollector {
		return collector.NewNetStatsCollector(client, ch)
	})

	near(t, gauge(t, text, "monero_net_rx_bytes"), 111111111)
	near(t, gauge(t, text, "monero_net_tx_bytes"), 222222222)
}

func assertConnections(t *testing.T, client *daemon.Client) {
	t.Helper()

	text := collectCustom(t, func(ch chan<- prometheus.Metric) collector.CustomCollector {
		return collector.NewConnectionsCollector(client, ch)
	})

	near(t, gaugeWith(t, text, "monero_p2p_connections", `state="normal",type="in"`), 1)
	near(t, gaugeWith(t, text, "monero_p2p_connections", `state="normal",type="out"`), 1)
	near(t, gaugeWith(t, text, "monero_p2p_connections", `state="synchronizing",type="out"`), 1)

	requireSummary(t, text, "monero_p2p_connections_age", 3, 340)
	requireSummary(t, text, "monero_p2p_connections_rx_rate_bps", 3, 100)
	requireSummary(t, text, "monero_p2p_connections_tx_rate_bps", 3, 65)
	requireSummary(t, text, "monero_p2p_connections_height", 3, 10499001)
}

func assertPeers(t *testing.T, client *daemon.Client) {
	t.Helper()

	text := collectCustom(t, func(ch chan<- prometheus.Metric) collector.CustomCollector {
		return collector.NewPeersCollector(client, ch)
	})

	near(t, gaugeWith(t, text, "monero_peerlist", `type="white"`), 2)
	near(t, gaugeWith(t, text, "monero_peerlist", `type="gray"`), 1)
	requireSummary(t, text, "monero_peerlist_lastseen", 2, 10800)
}

func feePerKBSum() float64 {
	const micro = 1_000_000

	perKB := func(fee, size float64) float64 {
		return (fee / micro) / (size / 1024)
	}

	return perKB(20_000_000_000, 100) + perKB(8_000_000_000, 150)
}

func TestSyncRatio(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		height uint64
		target uint64
		want   float64
	}{
		{name: "synced target is zero", height: 3500001, target: 0, want: 1},
		{name: "still syncing", height: 1000, target: 4000, want: 0.25},
		{name: "height reached target", height: 4000, target: 4000, want: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			text := collectCustom(t, func(ch chan<- prometheus.Metric) collector.CustomCollector {
				return collector.NewOverallCollector(patchedInfoClient(t, tc.height, tc.target), ch)
			})
			near(t, gauge(t, text, "monero_info_sync_ratio"), tc.want)
		})
	}
}

func patchedInfoClient(t *testing.T, height, target uint64) *daemon.Client {
	t.Helper()

	body := patchedInfo(t, height, target)
	srv := newFakeMonerod(t, withBody("get_info", body))

	return srv.client(t, "", "")
}

func patchedInfo(t *testing.T, height, target uint64) []byte {
	t.Helper()

	var info map[string]any
	if err := json.Unmarshal(loadFixtures(t)["get_info"], &info); err != nil {
		t.Fatalf("info: %v", err)
	}

	info["height"] = height
	info["target_height"] = target

	body, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal info: %v", err)
	}

	return body
}

func TestCollectorErrorClearsUp(t *testing.T) {
	t.Parallel()

	body, _ := scrapeServer(t, "", "", withStatus("get_peer_list", http.StatusInternalServerError))
	text := string(body)

	near(t, gauge(t, text, "monero_up"), 0)
	near(t, gauge(t, text, "monero_info_height"), 3500001)

	if bytes.Contains(body, []byte("monero_peerlist{")) || bytes.Contains(body, []byte("monero_peerlist ")) {
		t.Fatal("peer metrics were emitted after get_peer_list failed")
	}
}

func TestDigestRejectsWrongPassword(t *testing.T) {
	t.Parallel()

	srv := newFakeMonerod(t, withDigest(digestUser, digestPass))
	client := srv.client(t, digestUser, "wrong-password")

	if _, err := client.GetInfo(t.Context()); err == nil {
		t.Fatal("GetInfo succeeded with the wrong digest password")
	}

	if srv.authed.Load() != 0 {
		t.Fatalf("wrong password was accepted %d times", srv.authed.Load())
	}

	if srv.challenge.Load() == 0 {
		t.Fatal("server did not challenge the client")
	}
}

func TestMetricsGolden(t *testing.T) {
	t.Parallel()

	openBody, openSrv := scrapeServer(t, "", "")
	digestBody, digestSrv := scrapeServer(t, digestUser, digestPass)

	if !bytes.Equal(openBody, digestBody) {
		t.Fatalf("digest auth changed /metrics\n%s", lineDiff(openBody, digestBody))
	}

	if digestSrv.authed.Load() == 0 || digestSrv.authed.Load() != digestSrv.challenge.Load() {
		t.Fatalf("digest handshake authed=%d challenged=%d",
			digestSrv.authed.Load(), digestSrv.challenge.Load())
	}

	text := string(openBody)
	near(t, gauge(t, text, "monero_up"), 1)
	near(t, gauge(t, text, "monero_exporter_collection_duration_seconds"), 0)

	matchGolden(t, goldenPath, openBody)
	openSrv.assertSeenAll(t)

	findings, err := promtoolText(openBody)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}

	matchGolden(t, baselinePath, []byte(findings))
}
