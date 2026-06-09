// Package collector provides the core functionality of this exporter.
//
// It implements the Prometheus collector interface, providing `monero` metrics
// whenever a request hits this exporter. By default it queries monerod live on
// each scrape (relying on prometheus' scrape interval); with a refresh interval
// configured it instead refreshes in the background and serves a cached
// snapshot, decoupling scrape success from RPC latency.
package collector
