package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"
)

type OverallCollector struct {
	client   *daemon.Client
	metricsC chan<- prometheus.Metric

	info *daemon.GetInfoResult
}

var _ CustomCollector = (*OverallCollector)(nil)

func NewOverallCollector(
	client *daemon.Client, metricsC chan<- prometheus.Metric,
) *OverallCollector {
	return &OverallCollector{
		client:   client,
		metricsC: metricsC,
	}
}

func (c *OverallCollector) Name() string {
	return collectorOverall
}

func (c *OverallCollector) Collect(ctx context.Context) error {
	err := c.fetchData(ctx)
	if err != nil {
		return fmt.Errorf("fetch data: %w", err)
	}

	c.collect()

	return nil
}

func (c *OverallCollector) fetchData(ctx context.Context) error {
	res, err := c.client.GetInfo(ctx)
	if err != nil {
		return fmt.Errorf("get netstats: %w", err)
	}

	c.info = res

	return nil
}

//nolint:funlen // a long but flat sequence of independent metric emissions.
func (c *OverallCollector) collect() {
	now := time.Now()

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoUptime,
		prometheus.GaugeValue,
		//nolint:gosec // a unix start-time timestamp comfortably fits in int64.
		float64(now.
			Sub(time.Unix(int64(c.info.StartTime), 0)).
			Seconds()),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoAltBlocks,
		prometheus.GaugeValue,
		float64(c.info.AltBlocksCount),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoOffline,
		prometheus.GaugeValue,
		boolToFloat64(c.info.Offline),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoMainnet,
		prometheus.GaugeValue,
		boolToFloat64(c.info.Mainnet),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoBlockSizeLimit,
		prometheus.GaugeValue,
		float64(c.info.BlockSizeLimit),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoBlockSizeMedian,
		prometheus.GaugeValue,
		float64(c.info.BlockSizeMedian),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoSynchronized,
		prometheus.GaugeValue,
		boolToFloat64(c.info.Synchronized),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoHeight,
		prometheus.GaugeValue,
		float64(c.info.Height),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoTargetHeight,
		prometheus.GaugeValue,
		float64(c.info.TargetHeight),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoSyncRatio,
		prometheus.GaugeValue,
		syncRatio(c.info.Height, c.info.TargetHeight),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoRPCConnections,
		prometheus.GaugeValue,
		float64(c.info.RPCConnectionsCount),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoDatabaseSize,
		prometheus.GaugeValue,
		float64(c.info.DatabaseSize),
	)

	c.metricsC <- prometheus.MustNewConstMetric(
		descInfoFreeSpace,
		prometheus.GaugeValue,
		float64(c.info.FreeSpace),
	)
}

// syncRatio reports how far the chain has caught up, in [0,1]. monerod only
// advertises a target_height while it is still catching up and reports 0 once
// synced, so a zero (or already-reached) target is treated as fully synced.
func syncRatio(height, targetHeight uint64) float64 {
	if targetHeight == 0 || height >= targetHeight {
		return 1
	}

	return float64(height) / float64(targetHeight)
}

func boolToFloat64(b bool) float64 {
	if b {
		return 1
	}

	return 0
}
