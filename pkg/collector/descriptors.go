package collector

import "github.com/prometheus/client_golang/prometheus"

// Metric descriptors are allocated once. Describe sends every one of them so
// the Prometheus registry can reject duplicate names, inconsistent label sets,
// and HELP mismatches at registration time rather than at scrape time.
var (
	descUp = prometheus.NewDesc(
		"monero_up",
		"whether the exporter's last collection cycle reached monerod "+
			"without any collector error (1) or not (0)",
		nil, nil,
	)

	descCollectionDuration = prometheus.NewDesc(
		"monero_exporter_collection_duration_seconds",
		"wall-clock duration of the exporter's last collection cycle",
		nil, nil,
	)

	descInfoUptime = prometheus.NewDesc(
		"monero_info_uptime_seconds_total",
		"for how long this node has been up",
		nil, nil,
	)
	descInfoAltBlocks = prometheus.NewDesc(
		"monero_info_alternative_blocks",
		"number of blocks alternative to the longest",
		nil, nil,
	)
	descInfoOffline = prometheus.NewDesc(
		"monero_info_offline",
		"whether the node is offline",
		nil, nil,
	)
	descInfoMainnet = prometheus.NewDesc(
		"monero_info_mainnet",
		"whether the node is connected to mainnet",
		nil, nil,
	)
	descInfoBlockSizeLimit = prometheus.NewDesc(
		"monero_info_block_size_limit_bytes",
		"maximum hard limit of a block",
		nil, nil,
	)
	descInfoBlockSizeMedian = prometheus.NewDesc(
		"monero_info_block_size_median_bytes",
		"current median size for computing dynamic fees",
		nil, nil,
	)
	descInfoSynchronized = prometheus.NewDesc(
		"monero_info_synchronized",
		"whether the node's chain is in sync with the network",
		nil, nil,
	)
	descInfoHeight = prometheus.NewDesc(
		"monero_info_height",
		"current height of the chain",
		nil, nil,
	)
	descInfoTargetHeight = prometheus.NewDesc(
		"monero_info_target_height",
		"target height to achieve to be considered in sync",
		nil, nil,
	)
	descInfoSyncRatio = prometheus.NewDesc(
		"monero_info_sync_ratio",
		"chain sync progress as a ratio in [0,1] (1 = fully "+
			"synced); monerod reports target_height=0 once "+
			"synced, which this metric normalizes to 1",
		nil, nil,
	)
	descInfoRPCConnections = prometheus.NewDesc(
		"monero_info_rpc_connections",
		"number of rpc connections being served by the node",
		nil, nil,
	)
	descInfoDatabaseSize = prometheus.NewDesc(
		"monero_info_database_size_bytes",
		"size of the monero database",
		nil, nil,
	)
	descInfoFreeSpace = prometheus.NewDesc(
		"monero_info_free_space_bytes",
		"amount of free space in the partition where "+
			"monero's database is in",
		nil, nil,
	)

	descLastBlockSize = prometheus.NewDesc(
		"monero_lastblock_size_bytes",
		"total size of the last block",
		nil, nil,
	)
	descLastBlockDifficulty = prometheus.NewDesc(
		"monero_lastblock_difficulty",
		"difficulty used for the last block",
		nil, nil,
	)
	descLastBlockFees = prometheus.NewDesc(
		"monero_lastblock_fees_monero",
		"total amount of fees included in this block",
		nil, nil,
	)
	descLastBlockHeight = prometheus.NewDesc(
		"monero_lastblock_height",
		"height of the last block",
		nil, nil,
	)
	descLastBlockReward = prometheus.NewDesc(
		"monero_lastblock_reward_monero",
		"total amount of rewards granted in the last block "+
			"(subsidy + fees)",
		nil, nil,
	)
	descLastBlockSubsidy = prometheus.NewDesc(
		"monero_lastblock_subsidy_monero",
		"newly minted monero for this block",
		nil, nil,
	)
	descLastBlockTransactions = prometheus.NewDesc(
		"monero_lastblock_transactions",
		"number of transactions seen in the last block",
		nil, nil,
	)
	descLastBlockFeePerKB = prometheus.NewDesc(
		"monero_lastblock_fees_micronero_per_kb",
		"distribution of the feeperkb utilized for txns",
		nil, nil,
	)
	descLastBlockTxSize = prometheus.NewDesc(
		"monero_lastblock_transactions_size_bytes",
		"distribution of the size of the transactions included",
		nil, nil,
	)
	descLastBlockTxInputs = prometheus.NewDesc(
		"monero_lastblock_transactions_inputs",
		"distribution of inputs in the last block",
		nil, nil,
	)
	descLastBlockTxOutputs = prometheus.NewDesc(
		"monero_lastblock_transactions_outputs",
		"distribution of outputs in the last block",
		nil, nil,
	)
	// Kept for the unused version collector so those descriptors, if ever
	// emitted, are the same objects Describe already published.
	descLastBlockVersionMajor = prometheus.NewDesc(
		"monero_lastblock_version_major",
		"major version of the block format",
		nil, nil,
	)
	descLastBlockVersionMinor = prometheus.NewDesc(
		"monero_lastblock_version_minor",
		"minor version of the block format",
		nil, nil,
	)

	descTxPoolSpentKeyImages = prometheus.NewDesc(
		"monero_transaction_pool_spent_key_images",
		"total number of key images spent across all transactions"+
			" in the pool",
		nil, nil,
	)
	descTxPoolTransactions = prometheus.NewDesc(
		"monero_transaction_pool_transactions",
		"number of transactions in the pool at the moment of "+
			"the scrape",
		nil, nil,
	)
	descTxPoolSize = prometheus.NewDesc(
		"monero_transaction_pool_size_bytes",
		"total size of the transaction pool",
		nil, nil,
	)
	descTxPoolTxSize = prometheus.NewDesc(
		"monero_transaction_pool_transactions_size_bytes",
		"distribution of the size of the transactions "+
			"in the transaction pool",
		nil, nil,
	)
	descTxPoolFeePerKB = prometheus.NewDesc(
		"monero_transaction_pool_fees_micronero_per_kb",
		"distribution of the feeperkb utilized for txns"+
			" in the pool",
		nil, nil,
	)
	descTxPoolTxInputs = prometheus.NewDesc(
		"monero_transaction_pool_transactions_inputs",
		"distribution of inputs in the pool",
		nil, nil,
	)
	descTxPoolTxOutputs = prometheus.NewDesc(
		"monero_transaction_pool_transactions_outputs",
		"distribution of outputs in the pool",
		nil, nil,
	)
	descTxPoolTxAge = prometheus.NewDesc(
		"monero_transaction_pool_transactions_age",
		"distribution of for how long transactions have "+
			"been in the pool",
		nil, nil,
	)
	descTxPoolFees = prometheus.NewDesc(
		"monero_transaction_pool_fees_monero",
		"total amount of fee being spent in the transaction pool",
		nil, nil,
	)
	descTxPoolFailing = prometheus.NewDesc(
		"monero_transaction_pool_failing_transactions",
		"number of transactions that are marked as failing",
		nil, nil,
	)
	descTxPoolDoubleSpends = prometheus.NewDesc(
		"monero_transaction_pool_double_spends",
		"transactions doubly spending outputs",
		nil, nil,
	)
	descTxPoolNotRelayed = prometheus.NewDesc(
		"monero_transaction_pool_not_relayed",
		"number of transactions that have not been relayed",
		nil, nil,
	)
	descTxPoolOlderThan10m = prometheus.NewDesc(
		"monero_transaction_pool_older_than_10m",
		"number of transactions that are older than 10m",
		nil, nil,
	)

	descRPCHits = prometheus.NewDesc(
		"monero_rpc_hits_total",
		"number of hits that a particular rpc "+
			"method had since startup",
		[]string{"method"}, nil,
	)
	descRPCSeconds = prometheus.NewDesc(
		"monero_rpc_seconds_total",
		"amount of time spent service the method "+
			"since startup",
		[]string{"method"}, nil,
	)

	descConnAge = prometheus.NewDesc(
		"monero_p2p_connections_age",
		"distribution of age of the connections we have",
		nil, nil,
	)
	descConnRxRate = prometheus.NewDesc(
		"monero_p2p_connections_rx_rate_bps",
		"distribution of data receive rate in bytes/s",
		nil, nil,
	)
	descConnTxRate = prometheus.NewDesc(
		"monero_p2p_connections_tx_rate_bps",
		"distribution of data transmit rate in bytes/s",
		nil, nil,
	)
	descConnHeight = prometheus.NewDesc(
		"monero_p2p_connections_height",
		"distribution the height of the peers "+
			"connected to/from us",
		nil, nil,
	)
	descConnCount = prometheus.NewDesc(
		"monero_p2p_connections",
		"number of connections to/from this node",
		[]string{"type", "state"}, nil,
	)

	descPeerCount = prometheus.NewDesc(
		"monero_peerlist",
		"number of node entries in the peerlist",
		[]string{"type"}, nil,
	)
	descPeerLastSeen = prometheus.NewDesc(
		"monero_peerlist_lastseen",
		"distribution of when our peers have been seen",
		nil, nil,
	)

	descNetRx = prometheus.NewDesc(
		"monero_net_rx_bytes",
		"number of bytes received by this node",
		nil, nil,
	)
	descNetTx = prometheus.NewDesc(
		"monero_net_tx_bytes",
		"number of bytes sent by this node",
		nil, nil,
	)
)

// descriptors is every descriptor the exporter may emit, including the block
// version series collectVersions knows how to produce.
var descriptors = []*prometheus.Desc{
	descUp,
	descCollectionDuration,

	descInfoUptime,
	descInfoAltBlocks,
	descInfoOffline,
	descInfoMainnet,
	descInfoBlockSizeLimit,
	descInfoBlockSizeMedian,
	descInfoSynchronized,
	descInfoHeight,
	descInfoTargetHeight,
	descInfoSyncRatio,
	descInfoRPCConnections,
	descInfoDatabaseSize,
	descInfoFreeSpace,

	descLastBlockSize,
	descLastBlockDifficulty,
	descLastBlockFees,
	descLastBlockHeight,
	descLastBlockReward,
	descLastBlockSubsidy,
	descLastBlockTransactions,
	descLastBlockFeePerKB,
	descLastBlockTxSize,
	descLastBlockTxInputs,
	descLastBlockTxOutputs,
	descLastBlockVersionMajor,
	descLastBlockVersionMinor,

	descTxPoolSpentKeyImages,
	descTxPoolTransactions,
	descTxPoolSize,
	descTxPoolTxSize,
	descTxPoolFeePerKB,
	descTxPoolTxInputs,
	descTxPoolTxOutputs,
	descTxPoolTxAge,
	descTxPoolFees,
	descTxPoolFailing,
	descTxPoolDoubleSpends,
	descTxPoolNotRelayed,
	descTxPoolOlderThan10m,

	descRPCHits,
	descRPCSeconds,

	descConnAge,
	descConnRxRate,
	descConnTxRate,
	descConnHeight,
	descConnCount,

	descPeerCount,
	descPeerLastSeen,

	descNetRx,
	descNetTx,
}
