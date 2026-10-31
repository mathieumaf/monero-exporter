package collector_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil/promlint"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

type metricCollector struct {
	metrics []prometheus.Metric
}

func (c metricCollector) Describe(chan<- *prometheus.Desc) {}

func (c metricCollector) Collect(ch chan<- prometheus.Metric) {
	for _, m := range c.metrics {
		ch <- m
	}
}

func collectCustom(
	t *testing.T,
	build func(chan<- prometheus.Metric) collector.CustomCollector,
) string {
	t.Helper()

	ch := make(chan prometheus.Metric, 128)
	custom := build(ch)

	if err := custom.Collect(context.Background()); err != nil {
		t.Fatalf("collect %s: %v", custom.Name(), err)
	}

	close(ch)

	metrics := make([]prometheus.Metric, 0, 32)
	for metric := range ch {
		metrics = append(metrics, metric)
	}

	return string(scrapeMetrics(t, metricCollector{metrics: metrics}))
}

func scrapeMetrics(t *testing.T, c prometheus.Collector) []byte {
	t.Helper()

	reg := prometheus.NewRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(
		context.Background(), http.MethodGet, srv.URL+"/metrics", nil,
	)
	if err != nil {
		t.Fatalf("request: %v", err)
	}

	req.Header.Set("Accept", "text/plain;version=0.0.4")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics: %v", err)
	}

	return body
}

func scrapeServer(t *testing.T, user, pass string, opts ...func(*fakeMonerod)) ([]byte, *fakeMonerod) {
	t.Helper()

	if user != "" {
		opts = append(opts, withDigest(user, pass))
	}

	srv := newFakeMonerod(t, opts...)
	col, err := collector.New(srv.client(t, user, pass))
	if err != nil {
		t.Fatalf("collector: %v", err)
	}

	first := scrapeMetrics(t, col)
	second := scrapeMetrics(t, col)
	if !bytes.Equal(first, second) {
		t.Fatal("scrape output changed between calls")
	}

	return first, srv
}

func gauge(t *testing.T, text, name string) float64 {
	t.Helper()

	prefix := name + " "

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return parseLastFloat(t, line)
		}
	}

	t.Fatalf("missing series %s", name)

	return 0
}

func gaugeWith(t *testing.T, text, name, labels string) float64 {
	t.Helper()

	prefix := name + "{" + labels + "}"

	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return parseLastFloat(t, line)
		}
	}

	t.Fatalf("missing series %s", prefix)

	return 0
}

func requireSummary(t *testing.T, text, name string, count, sum float64) {
	t.Helper()

	near(t, gauge(t, text, name+"_count"), count)
	near(t, gauge(t, text, name+"_sum"), sum)
}

func parseLastFloat(t *testing.T, line string) float64 {
	t.Helper()

	fields := strings.Fields(line)
	if len(fields) == 0 {
		t.Fatal("empty metric line")
	}

	value, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil {
		t.Fatalf("parse %q: %v", line, err)
	}

	return value
}

func near(t *testing.T, got, want float64) {
	t.Helper()

	diff := math.Abs(got - want)
	base := math.Abs(want)
	if base < 1 {
		base = 1
	}

	if diff <= 1e-6 || diff/base <= 1e-9 {
		return
	}

	t.Fatalf("got %g want %g", got, want)
}

func matchGolden(t *testing.T, path string, got []byte) {
	t.Helper()

	if *updateGolden {
		if err := os.WriteFile(path, got, 0o644); err != nil { //nolint:gosec // repo testdata
			t.Fatalf("write %s: %v", path, err)
		}
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v (go test ./pkg/collector -update)", path, err)
	}

	if !bytes.Equal(want, got) {
		t.Fatalf("%s mismatch\n%s", path, lineDiff(want, got))
	}
}

func lineDiff(want, got []byte) string {
	left := strings.Split(string(want), "\n")
	right := strings.Split(string(got), "\n")
	limit := len(left)

	if len(right) > limit {
		limit = len(right)
	}

	var b strings.Builder

	shown := 0

	for i := 0; shown < 30 && i < limit; i++ {
		var l, r string
		if i < len(left) {
			l = left[i]
		}

		if i < len(right) {
			r = right[i]
		}

		if l == r {
			continue
		}

		fmt.Fprintf(&b, "line %d\n- %s\n+ %s\n", i+1, l, r)
		shown++
	}

	return b.String()
}

func promtoolText(body []byte) (string, error) {
	problems, err := promlint.New(bytes.NewReader(body)).Lint()
	if err != nil {
		return "", fmt.Errorf("promlint: %w", err)
	}

	var b strings.Builder

	for _, problem := range problems {
		fmt.Fprintf(&b, "%s %s\n", problem.Metric, problem.Text)
	}

	return b.String(), nil
}
