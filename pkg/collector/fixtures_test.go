package collector_test

import (
	"bytes"
	"encoding/json"
	"net"
	"testing"

	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"
)

func TestFixturesParse(t *testing.T) {
	t.Parallel()

	fx := loadFixtures(t)

	var info daemon.GetInfoResult
	mustJSON(t, fx[rpcGetInfo], &info)

	if info.Version != "0.18.4.4-release" {
		t.Fatalf("version %q", info.Version)
	}

	if info.StartTime != uint64(fixedNowUnix-86400) || info.Height != 3500001 {
		t.Fatalf("info start=%d height=%d", info.StartTime, info.Height)
	}

	var peers daemon.GetPeerListResult
	mustJSON(t, fx["get_peer_list"], &peers)
	assertPeerClock(t, peers)

	var conns daemon.GetConnectionsResult
	mustJSON(t, fx["get_connections"], &conns)
	assertLiveTimes(t, conns)

	assertBlockAndTxs(t, fx)
	assertPool(t, fx)

	mustJSON(t, fx["get_net_stats"], new(daemon.GetNetStatsResult))
	mustJSON(t, fx["get_transaction_pool_stats"], new(daemon.GetTransactionPoolStatsResult))
	mustJSON(t, fx["rpc_access_tracking"], new(daemon.RPCAccessTrackingResult))
	mustJSON(t, fx["get_last_block_header"], new(daemon.GetLastBlockHeaderResult))
}

func assertPeerClock(t *testing.T, peers daemon.GetPeerListResult) {
	t.Helper()

	if len(peers.WhiteList) != 2 || len(peers.GrayList) != 1 {
		t.Fatalf("peers white=%d gray=%d", len(peers.WhiteList), len(peers.GrayList))
	}

	if peers.WhiteList[0].LastSeen != fixedNowUnix-3600 {
		t.Fatalf("first last_seen %d", peers.WhiteList[0].LastSeen)
	}

	if peers.WhiteList[1].LastSeen != fixedNowUnix-7200 {
		t.Fatalf("second last_seen %d", peers.WhiteList[1].LastSeen)
	}
}

func assertLiveTimes(t *testing.T, conns daemon.GetConnectionsResult) {
	t.Helper()

	if len(conns.Connections) != 3 {
		t.Fatalf("connections %d", len(conns.Connections))
	}

	for _, conn := range conns.Connections {
		if conn.LiveTime == 0 {
			t.Fatalf("connection %s has live_time 0", conn.Address)
		}
	}
}

func assertBlockAndTxs(t *testing.T, fx map[string][]byte) {
	t.Helper()

	var block daemon.GetBlockResult
	mustJSON(t, fx["get_block"], &block)

	inner, err := block.InnerJSON()
	if err != nil {
		t.Fatalf("inner json: %v", err)
	}

	if len(inner.TxHashes) != 2 || block.BlockHeader.NumTxes != 2 {
		t.Fatalf("block txs header=%d hashes=%d", block.BlockHeader.NumTxes, len(inner.TxHashes))
	}

	var txs daemon.GetTransactionsResult
	mustJSON(t, fx["get_transactions"], &txs)

	parsed, err := txs.GetTransactions()
	if err != nil {
		t.Fatalf("transactions: %v", err)
	}

	if len(parsed) != 2 || parsed[0].RctSignatures.Txnfee != 20_000_000_000 {
		t.Fatalf("decoded %d transactions", len(parsed))
	}
}

func assertPool(t *testing.T, fx map[string][]byte) {
	t.Helper()

	var pool daemon.GetTransactionPoolResult
	mustJSON(t, fx["get_transaction_pool"], &pool)

	if len(pool.Transactions) != 2 || len(pool.SpentKeyImages) != 2 {
		t.Fatalf("pool txs=%d keyimages=%d", len(pool.Transactions), len(pool.SpentKeyImages))
	}

	for _, txn := range pool.Transactions {
		var decoded daemon.TransactionJSON
		if err := json.Unmarshal([]byte(txn.TxJSON), &decoded); err != nil {
			t.Fatalf("pool tx: %v", err)
		}

		if len(decoded.Vin) == 0 || txn.ReceiveTime == 0 {
			t.Fatalf("pool tx %s is incomplete", txn.IDHash)
		}
	}
}

func TestFixturesUseDocumentationIPs(t *testing.T) {
	t.Parallel()

	for name, body := range loadFixtures(t) {
		for _, loc := range ipv4Pattern.FindAllIndex(body, -1) {
			// monerod's version string is "0.18.4.4-release", which the IPv4
			// pattern also matches. A hyphen keeps it a version, not a host.
			if loc[1] < len(body) && body[loc[1]] == '-' {
				continue
			}

			ip := net.ParseIP(string(body[loc[0]:loc[1]]))
			if !isDocIP(ip) {
				t.Errorf("%s has non-documentation IP %s", name, body[loc[0]:loc[1]])
			}
		}

		if bytes.Contains(body, []byte(".onion")) || bytes.Contains(body, []byte(".i2p")) {
			t.Errorf("%s contains an overlay hostname", name)
		}
	}
}

func isDocIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	for _, network := range docNets {
		if network.Contains(ip) {
			return true
		}
	}

	return false
}
