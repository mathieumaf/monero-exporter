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
	CollectorOverall         = "overall"
	CollectorLastBlock       = "lastblock"
	CollectorTransactionPool = "transaction_pool"
	CollectorRPC             = "rpc"
	CollectorNet             = "net"
	CollectorConnections     = "connections"
	CollectorPeerlist        = "peerlist"
)

// collectorOrder is the sequential collection order. get_info (overall) stays
// first so it is never starved by the heavier peer and connection sweeps.
var collectorOrder = []string{
	CollectorOverall,
	CollectorLastBlock,
	CollectorTransactionPool,
	CollectorRPC,
	CollectorNet,
	CollectorConnections,
	CollectorPeerlist,
}

var (
	errUnknownCollector = errors.New("unknown collector")
	errCycleSlow        = errors.New("collection cycle exceeded the refresh interval")
)

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

	// enabled overrides the default (every collector on). A missing name is
	// enabled. intervals is the minimum time between runs; overall ignores it
	// and follows refreshInterval. Zero means every cycle.
	enabled   map[string]bool
	intervals map[string]time.Duration

	// mu guards cached, the last snapshot served in cache mode.
	mu     sync.RWMutex
	cached []prometheus.Metric

	// collectMu serializes a collection cycle and guards lastGood and lastRun.
	collectMu sync.Mutex
	// lastGood is the last successful sample set of each collector. A later
	// failure keeps those series in the scrape instead of removing them.
	lastGood map[string][]prometheus.Metric
	// lastRun is when each collector last succeeded, for per-collector intervals.
	lastRun map[string]time.Time

	// now, when set, replaces time.Now for interval scheduling.
	now func() time.Time

	// tipHash is top_block_hash from this cycle's get_info. blockHash and
	// blockMetrics reuse the last-block series until that hash changes.
	tipHash      string
	blockHash    string
	blockMetrics []prometheus.Metric

	// txPoolFull opts into get_transaction_pool for per-transaction series.
	txPoolFull bool

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

// WithCollectorEnabled turns a collector on or off. Collectors are enabled
// unless this option disables them. Names are the Collector* constants.
func WithCollectorEnabled(name string, enabled bool) Option {
	return func(c *Collector) {
		if c.enabled == nil {
			c.enabled = map[string]bool{}
		}

		c.enabled[name] = enabled
	}
}

// WithCollectorInterval sets the minimum time between successful runs of a
// collector. Cached samples are served in between. The overall collector
// ignores this and follows the global refresh interval. Zero collects every
// cycle.
func WithCollectorInterval(name string, interval time.Duration) Option {
	return func(c *Collector) {
		if c.intervals == nil {
			c.intervals = map[string]time.Duration{}
		}

		c.intervals[name] = interval
	}
}

// WithNow overrides the clock used for per-collector intervals.
func WithNow(now func() time.Time) Option {
	return func(c *Collector) {
		c.now = now
	}
}

// WithTransactionPoolFull opts into the full get_transaction_pool RPC so the
// per-transaction mempool distributions are exported. It is off by default
// because the response includes every pooled transaction body.
func WithTransactionPoolFull(full bool) Option {
	return func(c *Collector) {
		c.txPoolFull = full
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
	now := c.currentTime()
	c.tipHash = ""
	ok := true
	metrics := make([]prometheus.Metric, 0, 64)

	for _, name := range collectorOrder {
		got, failed := c.runCollector(ctx, name, now)
		if failed {
			ok = false
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

	c.noteCycle(time.Since(start))

	return metrics
}

// Refresh runs one collection cycle. When a refresh interval is configured the
// background loop calls this on each tick and scrapes serve the resulting
// snapshot.
func (c *Collector) Refresh(ctx context.Context) {
	c.refreshOnce(ctx)
}

func (c *Collector) noteCycle(elapsed time.Duration) {
	if c.refreshInterval <= 0 || elapsed <= c.refreshInterval {
		return
	}

	c.log.Error(
		errCycleSlow,
		"collection cycle exceeded refresh interval",
		"duration", elapsed.String(),
		"refresh_interval", c.refreshInterval.String(),
	)
}

// runCollector executes one collector when it is enabled and due. A disabled
// collector contributes nothing. A collector that is not due, or that fails
// after a previous success, contributes its cached samples.
func (c *Collector) runCollector(
	ctx context.Context, name string, now time.Time,
) ([]prometheus.Metric, bool) {
	if !c.collectorEnabled(name) {
		return nil, false
	}

	if !c.collectorDue(name, now) {
		return c.lastGood[name], false
	}

	got, err := c.collectOne(ctx, name)
	if err != nil {
		c.log.Error(err, "collect", "collector", name)

		return c.lastGood[name], true
	}

	c.storeGood(name, got)
	c.markRun(name, now)

	return got, false
}

func (c *Collector) collectorEnabled(name string) bool {
	if c.enabled == nil {
		return true
	}

	enabled, ok := c.enabled[name]
	if !ok {
		return true
	}

	return enabled
}

func (c *Collector) collectorDue(name string, now time.Time) bool {
	if name == CollectorOverall {
		return true
	}

	interval := c.intervals[name]
	if interval <= 0 {
		return true
	}

	last, ok := c.lastRun[name]
	if !ok || last.IsZero() {
		return true
	}

	return !now.Before(last.Add(interval))
}

func (c *Collector) markRun(name string, now time.Time) {
	if c.lastRun == nil {
		c.lastRun = make(map[string]time.Time, len(collectorOrder))
	}

	c.lastRun[name] = now
}

func (c *Collector) currentTime() time.Time {
	if c.now != nil {
		return c.now()
	}

	return time.Now()
}

// collectOne runs a single collector. The channel is drained on another
// goroutine only so a chatty collector cannot block on a full buffer; the
// caller waits for that collector to finish before starting the next one.
func (c *Collector) collectOne(
	ctx context.Context, name string,
) ([]prometheus.Metric, error) {
	if name == CollectorLastBlock {
		if cached := c.cachedBlock(); cached != nil {
			return cached, nil
		}
	}

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

	if name == CollectorLastBlock {
		c.rememberBlock(metrics)
	}

	return metrics, nil
}

func (c *Collector) cachedBlock() []prometheus.Metric {
	if c.tipHash == "" || c.tipHash != c.blockHash || len(c.blockMetrics) == 0 {
		return nil
	}

	return c.blockMetrics
}

func (c *Collector) rememberBlock(metrics []prometheus.Metric) {
	if c.tipHash == "" {
		return
	}

	saved := make([]prometheus.Metric, len(metrics))
	copy(saved, metrics)
	c.blockHash = c.tipHash
	c.blockMetrics = saved
}

func (c *Collector) newCollector(
	name string, ch chan<- prometheus.Metric,
) (CustomCollector, error) {
	switch name {
	case CollectorOverall:
		col := NewOverallCollector(c.client, ch)
		col.onInfo = func(info *daemon.GetInfoResult) {
			if info != nil {
				c.tipHash = info.TopBlockHash
			}
		}

		return col, nil
	case CollectorLastBlock:
		col := NewLastBlockStatsCollector(c.client, ch)
		col.knownHash = c.tipHash

		return col, nil
	case CollectorTransactionPool:
		col := NewTransactionPoolCollector(c.client, ch)
		col.full = c.txPoolFull

		return col, nil
	case CollectorRPC:
		return NewRPCCollector(c.client, ch), nil
	case CollectorNet:
		return NewNetStatsCollector(c.client, ch), nil
	case CollectorConnections:
		return NewConnectionsCollector(c.client, ch), nil
	case CollectorPeerlist:
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
