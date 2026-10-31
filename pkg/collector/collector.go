package collector

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"
)

// collectTimeout bounds how long a single collection cycle may take, so a hung
// RPC connection can never block a scrape (live mode) or freeze the background
// refresh loop (cache mode) indefinitely.
const collectTimeout = 2 * time.Minute

// Collector names match the CustomCollector.Name values and the
// --collector.<name> flags.
const (
	collectorOverall         = "overall"
	collectorLastBlock       = "lastblock"
	collectorTransactionPool = "transaction_pool"
	collectorRPC             = "rpc"
	collectorNet             = "net"
	collectorConnections     = "connections"
	collectorPeerlist        = "peerlist"
)

// collectorOrder is the sequential collection order. get_info (overall) stays
// first so it is never starved by the heavier peer and connection sweeps.
var collectorOrder = []string{
	collectorOverall,
	collectorLastBlock,
	collectorTransactionPool,
	collectorRPC,
	collectorNet,
	collectorConnections,
	collectorPeerlist,
}

var errUnknownCollector = errors.New("unknown collector")

// CountryMapper defines the signature of a function that given an IP,
// translates it into a country name.
//
//	f(ip) -> CN
type CountryMapper func(net.IP) (string, error)

// Collector implements the prometheus Collector interface, providing monero
// metrics whenever a prometheus scrape is received.
type Collector struct {
	// client is a Go client that communicated with a `monero` daemon via
	// plain HTTP(S) RPC.
	//
	client *daemon.Client

	// countryMapper is a function that knows how to translate IPs to
	// country codes.
	//
	// optional: if nil, no country-mapping will take place.
	//
	countryMapper CountryMapper

	// refreshInterval, when > 0, switches the collector into cache mode: a
	// background loop refreshes the metrics on this interval and every scrape
	// serves the last snapshot instantly. When 0, metrics are collected live
	// on each scrape.
	refreshInterval time.Duration

	// mu guards cached, the last snapshot served in cache mode.
	mu     sync.RWMutex
	cached []prometheus.Metric

	// collectMu serializes a collection cycle and guards lastGood.
	collectMu sync.Mutex
	// lastGood is the last successful sample set of each collector. A later
	// failure keeps those series in the scrape instead of removing them.
	lastGood map[string][]prometheus.Metric

	log logr.Logger
}

// ensure that we implement prometheus' collector interface.
var _ prometheus.Collector = &Collector{}

// Option is a type used by functional arguments to mutate the collector to
// override default behavior.
type Option func(c *Collector)

// WithCountryMapper is a functional argument that overrides the default no-op
// country mapper.
func WithCountryMapper(v CountryMapper) func(c *Collector) {
	return func(c *Collector) {
		c.countryMapper = v
	}
}

// WithRefreshInterval enables cache mode: instead of querying monerod live on
// every scrape, the collector refreshes its metrics in the background on the
// given interval and serves the last snapshot to each scrape. This decouples
// scrape success from RPC latency, so a slow daemon (e.g. during initial sync)
// no longer causes scrape timeouts and gaps. A zero or negative value keeps the
// default live-per-scrape behavior.
func WithRefreshInterval(v time.Duration) func(c *Collector) {
	return func(c *Collector) {
		c.refreshInterval = v
	}
}

// WithLogger overrides the default development logger.
func WithLogger(l logr.Logger) func(c *Collector) {
	return func(c *Collector) {
		c.log = l
	}
}

func defaultCountryMapper(_ net.IP) (string, error) {
	return "unknown", nil
}

// New builds a Collector without registering it. Register is the usual
// entry point; New exists so callers (and tests) can use a private registry.
func New(client *daemon.Client, opts ...Option) (*Collector, error) {
	defaultLogger, err := zap.NewDevelopment()
	if err != nil {
		return nil, fmt.Errorf("zap new development: %w", err)
	}

	c := &Collector{
		client:        client,
		countryMapper: defaultCountryMapper,
		log:           zapr.NewLogger(defaultLogger),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c, nil
}

// Register registers this collector with the global prometheus collectors
// registry making it available for an exporter to collect our metrics.
//
// When a refresh interval is configured (cache mode), it also starts the
// background refresh loop, which runs until ctx is cancelled.
func Register(ctx context.Context, client *daemon.Client, opts ...Option) error {
	c, err := New(client, opts...)
	if err != nil {
		return err
	}

	if err := prometheus.Register(c); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	if c.refreshInterval > 0 {
		go c.refreshLoop(ctx)
	}

	return nil
}

// CollectFunc defines a standardized signature for functions that want to
// expose metrics for collection.
type CollectFunc func(ctx context.Context, ch chan<- prometheus.Metric) error

// Describe implements the Describe function of the Collector interface.
//
// Every descriptor the exporter may emit is sent so registration takes the
// checked path: duplicate names, inconsistent label sets, and HELP mismatches
// fail when the collector is registered instead of at scrape time.
func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range descriptors {
		ch <- desc
	}
}

type CustomCollector interface {
	Name() string
	Collect(ctx context.Context) error
}

// Collect implements the Collect function of the Collector interface. In cache
// mode it serves the last background snapshot instantly; otherwise it collects
// live (see gather).
func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	if c.refreshInterval > 0 {
		c.mu.RLock()
		cached := c.cached
		c.mu.RUnlock()

		for _, m := range cached {
			ch <- m
		}

		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), collectTimeout)
	defer cancel()

	for _, m := range c.gather(ctx) {
		ch <- m
	}
}

// refreshLoop refreshes the cached snapshot on the configured interval until
// ctx is cancelled. The first refresh runs immediately so the cache is warm
// shortly after startup. Each cycle is bounded by collectTimeout; if a cycle
// runs longer than the interval, ticks simply coalesce (no overlap).
func (c *Collector) refreshLoop(ctx context.Context) {
	c.refreshOnce(ctx)

	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.refreshOnce(ctx)
		}
	}
}

func (c *Collector) refreshOnce(ctx context.Context) {
	cycleCtx, cancel := context.WithTimeout(ctx, collectTimeout)
	defer cancel()

	metrics := c.gather(cycleCtx)

	c.mu.Lock()
	c.cached = metrics
	c.mu.Unlock()
}

// gather runs every collector SEQUENTIALLY and returns the metrics they emit,
// plus the exporter's own health metrics (monero_up, collection duration).
//
// They run sequentially on purpose: monerod's RPC digest auth (epee http_auth)
// hands out a fresh nonce per challenge and keeps only a tiny nonce cache, and
// the digest transport challenges per request (unauth -> 401 -> auth). Firing
// the collectors concurrently makes the daemon evict not-yet-used nonces, so the
// heavier calls (get_connections, get_peer_list, ...) send their authenticated
// follow-up after their nonce is already gone and come back 401. One outstanding
// challenge at a time avoids the race entirely. A failing collector keeps the
// last series it successfully produced; it still flips monero_up to 0.
func (c *Collector) gather(ctx context.Context) []prometheus.Metric {
	c.collectMu.Lock()
	defer c.collectMu.Unlock()

	start := time.Now()
	ok := true
	metrics := make([]prometheus.Metric, 0, 64)

	for _, name := range collectorOrder {
		got, err := c.collectOne(ctx, name)
		if err != nil {
			ok = false
			c.log.Error(err, "collect", "collector", name)
			got = c.lastGood[name]
		} else {
			c.storeGood(name, got)
		}

		metrics = append(metrics, got...)
	}

	metrics = append(metrics,
		prometheus.MustNewConstMetric(
			descUp, prometheus.GaugeValue, boolToFloat64(ok),
		),
		prometheus.MustNewConstMetric(
			descCollectionDuration, prometheus.GaugeValue,
			time.Since(start).Seconds(),
		),
	)

	return metrics
}

// collectOne runs a single collector. The channel is drained on another
// goroutine only so a chatty collector cannot block on a full buffer; the
// caller waits for that collector to finish before starting the next one.
func (c *Collector) collectOne(
	ctx context.Context, name string,
) ([]prometheus.Metric, error) {
	buf := make(chan prometheus.Metric, 32)
	col, err := c.newCollector(name, buf)
	if err != nil {
		return nil, err
	}

	errc := make(chan error, 1)
	go func() {
		errc <- col.Collect(ctx)
		close(buf)
	}()

	metrics := make([]prometheus.Metric, 0, 8)
	for metric := range buf {
		metrics = append(metrics, metric)
	}

	if err := <-errc; err != nil {
		return nil, fmt.Errorf("collect %s: %w", name, err)
	}

	return metrics, nil
}

func (c *Collector) newCollector(
	name string, ch chan<- prometheus.Metric,
) (CustomCollector, error) {
	switch name {
	case collectorOverall:
		return NewOverallCollector(c.client, ch), nil
	case collectorLastBlock:
		return NewLastBlockStatsCollector(c.client, ch), nil
	case collectorTransactionPool:
		return NewTransactionPoolCollector(c.client, ch), nil
	case collectorRPC:
		return NewRPCCollector(c.client, ch), nil
	case collectorNet:
		return NewNetStatsCollector(c.client, ch), nil
	case collectorConnections:
		return NewConnectionsCollector(c.client, ch), nil
	case collectorPeerlist:
		return NewPeersCollector(c.client, ch), nil
	default:
		return nil, fmt.Errorf("%s: %w", name, errUnknownCollector)
	}
}

func (c *Collector) storeGood(name string, metrics []prometheus.Metric) {
	if c.lastGood == nil {
		c.lastGood = make(map[string][]prometheus.Metric, len(collectorOrder))
	}

	saved := make([]prometheus.Metric, len(metrics))
	copy(saved, metrics)
	c.lastGood[name] = saved
}
