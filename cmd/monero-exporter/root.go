package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-logr/zapr"
	"github.com/oschwald/geoip2-golang"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	mhttp "github.com/mathieumaf/go-monero/pkg/http"
	"github.com/mathieumaf/go-monero/pkg/rpc"
	"github.com/mathieumaf/go-monero/pkg/rpc/daemon"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
	"github.com/mathieumaf/monero-exporter/pkg/exporter"
)

// Environment variables consulted as a fallback for the RPC credentials, so
// that the password never has to appear in the process' command line (where it
// would be visible to anyone running `ps`). A flag, when set, takes precedence.
const (
	envRPCUser = "MONERO_RPC_USER"
	// #nosec G101 -- this is the env var NAME, not a credential.
	envRPCPassword = "MONERO_RPC_PASSWORD"
)

var errNonPositiveTimeout = errors.New("timeout must be positive")

type command struct {
	telemetryPath   string
	bindAddr        string
	geoIPFilepath   string
	moneroAddr      string
	rpcUser         string
	rpcPassword     string
	tlsSkipVerify   bool
	refreshInterval time.Duration
	rpcTimeout      time.Duration
	collectTimeout  time.Duration
	logLevel        string
	logFormat       string

	colOverall              bool
	colLastBlock            bool
	colLastBlockInterval    time.Duration
	colTransactionPool      bool
	colTransactionPoolEvery time.Duration
	colTransactionPoolFull  bool
	colRPC                  bool
	colRPCInterval          time.Duration
	colNet                  bool
	colNetInterval          time.Duration
	colConnections          bool
	colConnectionsInterval  time.Duration
	colPeerlist             bool
	colPeerlistInterval     time.Duration
}

func (c *command) Cmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monero-exporter",
		Short: "Prometheus exporter for monero metrics",
		RunE:  c.RunE,
	}

	cmd.Flags().StringVar(&c.bindAddr, "bind-addr",
		":9000", "address to bind the prometheus server to")

	cmd.Flags().StringVar(&c.telemetryPath, "telemetry-path",
		"/metrics", "endpoint at which prometheus metrics are served")

	cmd.Flags().StringVar(&c.moneroAddr, "monero-addr",
		"http://localhost:18081", "address of the monero instance to "+
			"collect info from")

	cmd.Flags().StringVar(&c.geoIPFilepath, "geoip-filepath",
		"", "filepath of a geoip database file for ip to country "+
			"resolution")
	_ = cmd.MarkFlagFilename("geoip-filepath")

	cmd.Flags().StringVar(&c.rpcUser, "monero-rpc-user",
		"", "username for monerod's RPC digest authentication "+
			"(matches the first half of monerod's --rpc-login); "+
			"falls back to the "+envRPCUser+" environment variable")

	cmd.Flags().StringVar(&c.rpcPassword, "monero-rpc-password",
		"", "password for monerod's RPC digest authentication "+
			"(matches the second half of monerod's --rpc-login); "+
			"prefer the "+envRPCPassword+" environment variable to "+
			"keep the secret out of the process command line")

	cmd.Flags().BoolVar(&c.tlsSkipVerify, "tls-skip-verify",
		false, "skip TLS certificate verification when monero-addr "+
			"is an https endpoint")

	cmd.Flags().DurationVar(&c.refreshInterval, "refresh-interval",
		0, "when > 0, collect metrics from monerod in the background on "+
			"this interval and serve the cached snapshot to every "+
			"scrape, instead of querying live per scrape; decouples "+
			"scrape success from RPC latency (e.g. during sync)")

	c.addRuntimeFlags(cmd)
	c.addCollectorFlags(cmd)
	cmd.Flags().BoolVar(&c.colTransactionPoolFull, "collector.transaction_pool.full",
		false, "fetch the full transaction pool (get_transaction_pool) for "+
			"per-transaction distributions; expensive")

	return cmd
}

// collectorToggle is a bool flag value. The negate form implements
// --no-collector.<name> against the same variable as --collector.<name>.
type collectorToggle struct {
	value  *bool
	negate bool
}

func (t collectorToggle) String() string {
	if t.value == nil {
		return "false"
	}

	if t.negate {
		return strconv.FormatBool(!*t.value)
	}

	return strconv.FormatBool(*t.value)
}

func (t collectorToggle) Set(raw string) error {
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("parse bool: %w", err)
	}

	if t.negate {
		*t.value = !parsed

		return nil
	}

	*t.value = parsed

	return nil
}

func (t collectorToggle) Type() string { return "bool" }

func (t collectorToggle) IsBoolFlag() bool { return true }

type collectorFlag struct {
	name     string
	enabled  *bool
	interval *time.Duration
	usage    string
}

func (c *command) addRuntimeFlags(cmd *cobra.Command) {
	cmd.Flags().DurationVar(&c.rpcTimeout, "rpc-timeout",
		defaultRPCTimeout, "timeout for each monerod RPC call")

	cmd.Flags().DurationVar(&c.collectTimeout, "collect-timeout",
		collector.DefaultCollectTimeout, "maximum duration of one collection cycle")
	// Duration.String renders 2m as "2m0s"; show the flag the way users type it.
	cmd.Flags().Lookup("collect-timeout").DefValue = "2m"

	cmd.Flags().StringVar(&c.logLevel, "log.level",
		defaultLogLevel, "log level (debug, info, warn, error)")

	cmd.Flags().StringVar(&c.logFormat, "log.format",
		logFormatJSON, "log format (json, console)")
}

func (c *command) addCollectorFlags(cmd *cobra.Command) {
	specs := []collectorFlag{
		{
			collector.CollectorOverall, &c.colOverall, nil,
			"collect general node info and peer/connection counts from get_info",
		},
		{
			collector.CollectorLastBlock, &c.colLastBlock, &c.colLastBlockInterval,
			"collect last-block statistics",
		},
		{
			collector.CollectorTransactionPool, &c.colTransactionPool, &c.colTransactionPoolEvery,
			"collect transaction-pool statistics",
		},
		{
			collector.CollectorRPC, &c.colRPC, &c.colRPCInterval,
			"collect RPC access statistics",
		},
		{
			collector.CollectorNet, &c.colNet, &c.colNetInterval,
			"collect aggregated network counters",
		},
		{
			collector.CollectorConnections, &c.colConnections, &c.colConnectionsInterval,
			"collect per-connection age, height, and rate distributions",
		},
		{
			collector.CollectorPeerlist, &c.colPeerlist, &c.colPeerlistInterval,
			"collect peer last-seen ages via get_peer_list (expensive on slow disks)",
		},
	}

	for _, spec := range specs {
		addCollectorToggle(cmd, spec.name, spec.enabled, spec.usage)
		if spec.interval == nil {
			continue
		}

		cmd.Flags().DurationVar(
			spec.interval,
			"collector."+spec.name+".interval",
			0,
			"minimum time between "+spec.name+" collections; 0 collects on every cycle",
		)
	}
}

func addCollectorToggle(cmd *cobra.Command, name string, enabled *bool, usage string) {
	*enabled = true

	positive := cmd.Flags().VarPF(
		collectorToggle{value: enabled},
		"collector."+name,
		"",
		usage,
	)
	positive.NoOptDefVal = "true"

	negative := cmd.Flags().VarPF(
		collectorToggle{value: enabled, negate: true},
		"no-collector."+name,
		"",
		"disable the "+name+" collector",
	)
	negative.NoOptDefVal = "true"
}

// resolveRPCCredentials returns the RPC username and password, preferring the
// flags and falling back to the environment variables. An empty pair means no
// authentication is configured (the daemon is reached without credentials).
func (c *command) resolveRPCCredentials() (user, password string) {
	user, password = c.rpcUser, c.rpcPassword

	if user == "" {
		user = os.Getenv(envRPCUser)
	}

	if password == "" {
		password = os.Getenv(envRPCPassword)
	}

	return user, password
}

// newDaemonClient builds a monerod daemon RPC client, wiring HTTP digest
// authentication when RPC credentials are configured (via flags or env vars).
func (c *command) newDaemonClient() (*daemon.Client, error) {
	rpcUser, rpcPassword := c.resolveRPCCredentials()

	httpClient, err := mhttp.NewClient(mhttp.ClientConfig{
		Username:       rpcUser,
		Password:       rpcPassword,
		TLSSkipVerify:  c.tlsSkipVerify,
		RequestTimeout: c.rpcTimeout,
	})
	if err != nil {
		return nil, fmt.Errorf("new http client: %w", err)
	}

	rpcClient, err := rpc.NewClient(c.moneroAddr,
		rpc.WithHTTPClient(httpClient),
	)
	if err != nil {
		return nil, fmt.Errorf("new client '%s': %w", c.moneroAddr, err)
	}

	return daemon.NewClient(rpcClient), nil
}

// collectorOptions returns the collector options derived from the command's
// configuration, opening the GeoIP database when a filepath was supplied. The
// returned cleanup closes any resources held by the options.
func (c *command) collectorOptions() (opts []collector.Option, cleanup func(), err error) {
	cleanup = func() {}

	if c.geoIPFilepath == "" {
		return opts, cleanup, nil
	}

	db, err := geoip2.Open(c.geoIPFilepath)
	if err != nil {
		return nil, cleanup, fmt.Errorf("geoip open: %w", err)
	}
	cleanup = func() { _ = db.Close() }

	countryMapper := func(ip net.IP) (string, error) {
		res, err := db.Country(ip)
		if err != nil {
			return "", fmt.Errorf("country '%s': %w", ip, err)
		}

		return res.RegisteredCountry.IsoCode, nil
	}

	opts = append(opts, collector.WithCountryMapper(countryMapper))

	return opts, cleanup, nil
}

func (c *command) collectorFlagOptions() []collector.Option {
	return []collector.Option{
		collector.WithCollectorEnabled(collector.CollectorOverall, c.colOverall),
		collector.WithCollectorEnabled(collector.CollectorLastBlock, c.colLastBlock),
		collector.WithCollectorInterval(collector.CollectorLastBlock, c.colLastBlockInterval),
		collector.WithCollectorEnabled(collector.CollectorTransactionPool, c.colTransactionPool),
		collector.WithCollectorInterval(collector.CollectorTransactionPool, c.colTransactionPoolEvery),
		collector.WithTransactionPoolFull(c.colTransactionPoolFull),
		collector.WithCollectorEnabled(collector.CollectorRPC, c.colRPC),
		collector.WithCollectorInterval(collector.CollectorRPC, c.colRPCInterval),
		collector.WithCollectorEnabled(collector.CollectorNet, c.colNet),
		collector.WithCollectorInterval(collector.CollectorNet, c.colNetInterval),
		collector.WithCollectorEnabled(collector.CollectorConnections, c.colConnections),
		collector.WithCollectorInterval(collector.CollectorConnections, c.colConnectionsInterval),
		collector.WithCollectorEnabled(collector.CollectorPeerlist, c.colPeerlist),
		collector.WithCollectorInterval(collector.CollectorPeerlist, c.colPeerlistInterval),
	}
}

func (c *command) validate() error {
	if c.rpcTimeout <= 0 {
		return fmt.Errorf("rpc timeout %s: %w", c.rpcTimeout, errNonPositiveTimeout)
	}

	if c.collectTimeout <= 0 {
		return fmt.Errorf("collect timeout %s: %w", c.collectTimeout, errNonPositiveTimeout)
	}

	return nil
}

func (c *command) warnTimeouts(logger *zap.Logger) {
	if c.rpcTimeout <= c.collectTimeout {
		return
	}

	logger.Warn(
		"rpc timeout exceeds collect timeout; a single call can consume the whole cycle budget",
		zap.Duration("rpc_timeout", c.rpcTimeout),
		zap.Duration("collect_timeout", c.collectTimeout),
	)
}

func commandContext(cmd *cobra.Command) context.Context {
	if cmd == nil {
		return context.Background()
	}

	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}

	return context.Background()
}

func (c *command) RunE(cmd *cobra.Command, _ []string) error {
	if err := c.validate(); err != nil {
		return err
	}

	logger, err := newLogger(c.logLevel, c.logFormat)
	if err != nil {
		return err
	}
	defer func() { _ = logger.Sync() }()

	c.warnTimeouts(logger)

	ctx, stop := signal.NotifyContext(commandContext(cmd), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return c.serve(ctx, logger)
}

func (c *command) serve(ctx context.Context, logger *zap.Logger) error {
	log := zapr.NewLogger(logger)

	daemonClient, err := c.newDaemonClient()
	if err != nil {
		return err
	}

	collectorOpts, cleanup, err := c.collectorOptions()
	if err != nil {
		return err
	}
	defer cleanup()

	collectorOpts = append(collectorOpts,
		collector.WithRefreshInterval(c.refreshInterval),
		collector.WithCollectTimeout(c.collectTimeout),
		collector.WithLogger(log.WithName("collector")),
	)
	collectorOpts = append(collectorOpts, c.collectorFlagOptions()...)

	if err := collector.Register(ctx, daemonClient, collectorOpts...); err != nil {
		return fmt.Errorf("collector register: %w", err)
	}

	prometheusExporter, err := exporter.New(
		exporter.WithBindAddress(c.bindAddr),
		exporter.WithTelemetryPath(c.telemetryPath),
		exporter.WithLogger(log.WithName("exporter")),
	)
	if err != nil {
		return fmt.Errorf("new exporter: %w", err)
	}
	defer func() {
		if closeErr := prometheusExporter.Close(); closeErr != nil {
			logger.Error("close exporter", zap.Error(closeErr))
		}
	}()

	// A cancelled process context is a successful shutdown, including when
	// Listen itself was interrupted by SIGINT/SIGTERM.
	err = prometheusExporter.Run(ctx)
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("prometheus exporter run: %w", err)
	}

	return nil
}
