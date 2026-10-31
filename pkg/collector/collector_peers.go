package collector

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"
)

type PeersCollector struct {
	client   *daemon.Client
	metricsC chan<- prometheus.Metric

	whitelist []daemon.Peer
}

var _ CustomCollector = (*PeersCollector)(nil)

func NewPeersCollector(
	client *daemon.Client, metricsC chan<- prometheus.Metric,
) *PeersCollector {
	return &PeersCollector{
		client:   client,
		metricsC: metricsC,
	}
}

func (c *PeersCollector) Name() string {
	return CollectorPeerlist
}

func (c *PeersCollector) Collect(ctx context.Context) error {
	err := c.fetchData(ctx)
	if err != nil {
		return fmt.Errorf("fetch data: %w", err)
	}

	c.collectPeersLastSeen()

	return nil
}

func (c *PeersCollector) fetchData(ctx context.Context) error {
	resp, err := c.client.GetPeerList(ctx)
	if err != nil {
		return fmt.Errorf("get peerlist: %w", err)
	}

	c.whitelist = resp.WhiteList

	return nil
}

func (c *PeersCollector) collectPeersLastSeen() {
	now := clockNow()
	summary := NewSummary()

	for _, peer := range c.whitelist {
		summary.Insert(now.
			Sub(time.Unix(peer.LastSeen, 0)).
			Seconds())
	}

	c.metricsC <- prometheus.MustNewConstSummary(
		descPeerLastSeen,
		summary.Count(), summary.Sum(), summary.Quantiles(),
	)
}
