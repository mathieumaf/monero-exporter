package collector

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mathieumaf/go-monero/pkg/constant"
	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"
)

type LastBlockStatsCollector struct {
	client   *daemon.Client
	metricsC chan<- prometheus.Metric

	txns     []*daemon.TransactionJSON
	txnSizes []int
	header   daemon.BlockHeader
}

var _ CustomCollector = (*LastBlockStatsCollector)(nil)

func NewLastBlockStatsCollector(
	client *daemon.Client, metricsC chan<- prometheus.Metric,
) *LastBlockStatsCollector {
	return &LastBlockStatsCollector{
		client:   client,
		metricsC: metricsC,
	}
}

func (c *LastBlockStatsCollector) Name() string {
	return "lastblock"
}

func (c *LastBlockStatsCollector) Collect(ctx context.Context) error {
	err := c.fetchData(ctx)
	if err != nil {
		return fmt.Errorf("fetch last block data: %w", err)
	}

	c.collectBlockSize()
	c.collectDifficulty()
	c.collectFees()
	c.collectHeight()
	c.collectReward()
	c.collectSubsidy()
	c.collectTransactionsCount()
	c.collectTransactionsFeePerKb()
	c.collectTransactionsInputs()
	c.collectTransactionsOutputs()
	c.collectTransactionsSize()

	return nil
}

func (c *LastBlockStatsCollector) fetchData(ctx context.Context) error {
	lastBlockHeaderResp, err := c.client.GetLastBlockHeader(ctx)
	if err != nil {
		return fmt.Errorf("get last block header: %w", err)
	}

	lastBlockHash := lastBlockHeaderResp.BlockHeader.Hash

	params := daemon.GetBlockRequestParameters{
		Hash: lastBlockHash,
	}
	blockResp, err := c.client.GetBlock(ctx, params)
	if err != nil {
		return fmt.Errorf("get block '%s': %w", lastBlockHash, err)
	}

	blockJSON, err := blockResp.InnerJSON()
	if err != nil {
		return fmt.Errorf("block inner json: %w", err)
	}

	txnsResp, err := c.client.GetTransactions(ctx, blockJSON.TxHashes)
	if err != nil {
		return fmt.Errorf("get txns: %w", err)
	}

	txnSizes := make([]int, len(txnsResp.Txs))
	for idx, t := range txnsResp.Txs {
		txnSizes[idx] = len(t.AsHex) / 2
	}

	txns, err := txnsResp.GetTransactions()
	if err != nil {
		return fmt.Errorf("get transactions: %w", err)
	}

	c.txns = txns
	c.txnSizes = txnSizes
	c.header = blockResp.BlockHeader

	return nil
}

func (c *LastBlockStatsCollector) collectBlockSize() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockSize,
		prometheus.GaugeValue,
		float64(c.header.BlockSize),
	)
}

func (c *LastBlockStatsCollector) collectDifficulty() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockDifficulty,
		prometheus.GaugeValue,
		float64(c.header.Difficulty),
	)
}

func (c *LastBlockStatsCollector) collectFees() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockFees,
		prometheus.GaugeValue,
		float64(c.gatherFees(c.txns))/constant.XMR,
	)
}

func (c *LastBlockStatsCollector) collectHeight() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockHeight,
		prometheus.GaugeValue,
		float64(c.header.Height),
	)
}

func (c *LastBlockStatsCollector) collectReward() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockReward,
		prometheus.GaugeValue,
		float64(c.header.Reward)/constant.XMR,
	)
}

func (c *LastBlockStatsCollector) collectSubsidy() {
	totalReward := float64(c.header.Reward)
	fees := float64(c.gatherFees(c.txns))
	subsidy := (totalReward - fees) / constant.XMR

	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockSubsidy,
		prometheus.GaugeValue,
		subsidy,
	)
}

func (c *LastBlockStatsCollector) collectTransactionsCount() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockTransactions,
		prometheus.GaugeValue,
		float64(c.header.NumTxes),
	)
}

func (c *LastBlockStatsCollector) collectTransactionsFeePerKb() {
	summary := NewSummary()
	for idx, txn := range c.txns {
		fee := float64(txn.RctSignatures.Txnfee) / constant.MicroXMR
		size := float64(c.txnSizes[idx]) / 1024

		summary.Insert(fee / size)
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descLastBlockFeePerKB,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

func (c *LastBlockStatsCollector) collectTransactionsSize() {
	summary := NewSummary()
	for _, size := range c.txnSizes {
		summary.Insert(float64(size))
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descLastBlockTxSize,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

func (c *LastBlockStatsCollector) collectTransactionsInputs() {
	summary := NewSummary()
	for _, txn := range c.txns {
		summary.Insert(float64(len(txn.Vin)))
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descLastBlockTxInputs,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

func (c *LastBlockStatsCollector) collectTransactionsOutputs() {
	summary := NewSummary()
	for _, txn := range c.txns {
		summary.Insert(float64(len(txn.Vout)))
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descLastBlockTxOutputs,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

//nolint:unused // kept for parity with the other per-field collectors; wired up on demand.
func (c *LastBlockStatsCollector) collectVersions() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockVersionMajor,
		prometheus.GaugeValue,
		float64(c.header.MajorVersion),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descLastBlockVersionMinor,
		prometheus.GaugeValue,
		float64(c.header.MinorVersion),
	)
}

func (c *LastBlockStatsCollector) gatherFees(txns []*daemon.TransactionJSON) uint64 {
	fees := uint64(0)
	for _, txn := range txns {
		fees += txn.RctSignatures.Txnfee
	}

	return fees
}
