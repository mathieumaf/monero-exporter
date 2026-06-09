package main

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/oschwald/geoip2-golang"
	"github.com/spf13/cobra"

	mhttp "github.com/cirocosta/go-monero/pkg/http"
	"github.com/cirocosta/go-monero/pkg/rpc"
	"github.com/cirocosta/go-monero/pkg/rpc/daemon"

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

type command struct {
	telemetryPath string
	bindAddr      string
	geoIPFilepath string
	moneroAddr    string
	rpcUser       string
	rpcPassword   string
	tlsSkipVerify bool
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

	return cmd
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
		Username:      rpcUser,
		Password:      rpcPassword,
		TLSSkipVerify: c.tlsSkipVerify,
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

func (c *command) RunE(_ *cobra.Command, _ []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	daemonClient, err := c.newDaemonClient()
	if err != nil {
		return err
	}

	collectorOpts, cleanup, err := c.collectorOptions()
	if err != nil {
		return err
	}
	defer cleanup()

	if err := collector.Register(daemonClient, collectorOpts...); err != nil {
		return fmt.Errorf("collector register: %w", err)
	}

	prometheusExporter, err := exporter.New(
		exporter.WithBindAddress(c.bindAddr),
		exporter.WithTelemetryPath(c.telemetryPath),
	)
	if err != nil {
		return fmt.Errorf("new exporter: %w", err)
	}
	defer prometheusExporter.Close()

	err = prometheusExporter.Run(ctx)
	if err != nil {
		return fmt.Errorf("prometheus exporter run: %w", err)
	}

	return nil
}
