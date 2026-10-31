package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mathieumaf/go-monero/pkg/constant"
	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"
)

type TransactionPoolCollector struct {
	client   *daemon.Client
	metricsC chan<- prometheus.Metric

	txns  []*daemon.TransactionJSON
	pool  *daemon.GetTransactionPoolResult
	stats *daemon.GetTransactionPoolStatsResult
}

var _ CustomCollector = (*TransactionPoolCollector)(nil)

func NewTransactionPoolCollector(
	client *daemon.Client, metricsC chan<- prometheus.Metric,
) *TransactionPoolCollector {
	return &TransactionPoolCollector{
		client:   client,
		metricsC: metricsC,
	}
}

func (c *TransactionPoolCollector) Name() string {
	return collectorTransactionPool
}

func (c *TransactionPoolCollector) Collect(ctx context.Context) error {
	err := c.fetchData(ctx)
	if err != nil {
		return fmt.Errorf("fetch data: %w", err)
	}

	c.collectSpentKeyImages()
	c.collectSize()
	c.collectTransactionsSize()
	c.collectTransactionsCount()
	c.collectTransactionsFee()
	c.collectTransactionsFeePerKb()
	c.collectTransactionsInputs()
	c.collectTransactionsOutputs()
	c.collectTransactionsAgeDistribution()
	c.collectWeirdCases()

	return nil
}

func (c *TransactionPoolCollector) fetchData(ctx context.Context) error {
	stats, err := c.client.GetTransactionPoolStats(ctx)
	if err != nil {
		return fmt.Errorf("get transactionpool stats: %w", err)
	}

	pool, err := c.client.GetTransactionPool(ctx)
	if err != nil {
		return fmt.Errorf("get transaction pool: %w", err)
	}

	c.stats = stats
	c.pool = pool

	c.txns = make([]*daemon.TransactionJSON, len(pool.Transactions))

	for idx, txn := range c.pool.Transactions {
		c.txns[idx] = new(daemon.TransactionJSON)

		err := json.Unmarshal([]byte(txn.TxJSON), c.txns[idx])
		if err != nil {
			return fmt.Errorf("unmarhsal tx json: %w", err)
		}
	}

	return nil
}

func (c *TransactionPoolCollector) collectSpentKeyImages() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descTxPoolSpentKeyImages,
		prometheus.GaugeValue,
		float64(len(c.pool.SpentKeyImages)),
	)
}

func (c *TransactionPoolCollector) collectTransactionsCount() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descTxPoolTransactions,
		prometheus.GaugeValue,
		float64(len(c.pool.Transactions)),
	)
}

func (c *TransactionPoolCollector) collectSize() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descTxPoolSize,
		prometheus.GaugeValue,
		float64(c.stats.PoolStats.BytesTotal),
	)
}

func (c *TransactionPoolCollector) collectTransactionsSize() {
	summary := NewSummary()
	for _, txn := range c.pool.Transactions {
		summary.Insert(float64(txn.BlobSize))
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descTxPoolTxSize,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

func (c *TransactionPoolCollector) collectTransactionsFeePerKb() {
	summary := NewSummary()
	for _, txn := range c.pool.Transactions {
		fee := float64(txn.Fee) / constant.MicroXMR
		size := float64(txn.BlobSize) / 1024

		summary.Insert(fee / size)
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descTxPoolFeePerKB,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

func (c *TransactionPoolCollector) collectTransactionsInputs() {
	summary := NewSummary()
	for _, txn := range c.txns {
		summary.Insert(float64(len(txn.Vin)))
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descTxPoolTxInputs,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

func (c *TransactionPoolCollector) collectTransactionsOutputs() {
	summary := NewSummary()
	for _, txn := range c.txns {
		summary.Insert(float64(len(txn.Vout)))
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descTxPoolTxOutputs,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

func (c *TransactionPoolCollector) collectTransactionsAgeDistribution() {
	now := time.Now()

	summary := NewSummary()
	for _, txn := range c.pool.Transactions {
		summary.Insert(
			now.Sub(time.Unix(txn.ReceiveTime, 0)).Seconds(),
		)
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descTxPoolTxAge,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}

func (c *TransactionPoolCollector) collectTransactionsFee() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descTxPoolFees,
		prometheus.GaugeValue,
		float64(c.stats.PoolStats.FeeTotal)/constant.XMR,
	)
}

func (c *TransactionPoolCollector) collectWeirdCases() {
	c.metricsC <- prometheus.MustNewConstMetric(
		descTxPoolFailing,
		prometheus.GaugeValue,
		float64(c.stats.PoolStats.NumFailing),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descTxPoolDoubleSpends,
		prometheus.GaugeValue,
		float64(c.stats.PoolStats.NumDoubleSpends),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descTxPoolNotRelayed,
		prometheus.GaugeValue,
		float64(c.stats.PoolStats.NumNotRelayed),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descTxPoolOlderThan10m,
		prometheus.GaugeValue,
		float64(c.stats.PoolStats.Num10M),
	)
}
