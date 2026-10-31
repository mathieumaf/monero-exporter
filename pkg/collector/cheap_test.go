package collector_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

func TestLastBlockRefetchesOnlyWhenTipChanges(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	c := newCollector(f, collector.WithLogger(logr.Discard()))
	reg := prometheus.NewPedanticRegistry()
	if err := reg.Register(c); err != nil {
		t.Fatalf("register: %v", err)
	}

	first := mustGather(t, reg)
	second := mustGather(t, reg)
	assertGauge(t, second, "monero_lastblock_height", nil, fixtureHeight)

	if got := f.jsonCallsFor("get_block"); got != 1 {
		t.Fatalf("get_block calls with unchanged tip = %d, want 1", got)
	}

	if got := f.rawCallsFor("/get_transactions"); got != 1 {
		t.Fatalf("get_transactions calls with unchanged tip = %d, want 1", got)
	}

	if got := f.jsonCallsFor("get_last_block_header"); got != 0 {
		t.Fatalf("get_last_block_header calls = %d, want 0 when get_info has the tip", got)
	}

	f.setJSON(rpcGetInfo, map[string]any{
		jsonHeight:  fixtureHeight,
		jsonTopHash: fixtureTop + "ff",
	})
	third := mustGather(t, reg)
	assertGauge(t, first, "monero_lastblock_height", nil, fixtureHeight)
	assertGauge(t, third, "monero_lastblock_height", nil, fixtureHeight)

	if got := f.jsonCallsFor("get_block"); got != 2 {
		t.Fatalf("get_block calls after tip change = %d, want 2", got)
	}
}

func TestTransactionPoolStatsAreTheDefault(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	families := gather(t, newCollector(f))

	if got := f.rawCallsFor("/get_transaction_pool_stats"); got != 1 {
		t.Fatalf("stats calls = %d, want 1", got)
	}

	if got := f.rawCallsFor("/get_transaction_pool"); got != 0 {
		t.Fatalf("full pool calls = %d, want 0", got)
	}

	assertGauge(t, families, "monero_transaction_pool_size_bytes", nil, 2048)
	assertGauge(t, families, "monero_transaction_pool_transactions", nil, 1)
	assertGauge(t, families, "monero_transaction_pool_not_relayed", nil, 1)
	if familyByName(families, "monero_transaction_pool_spent_key_images") != nil {
		t.Fatal("per-transaction pool series were exported without the full opt-in")
	}
}

func TestTransactionPoolFullOptIn(t *testing.T) {
	t.Parallel()

	f := newFixtureDaemon()
	families := gather(t, newCollector(f, collector.WithTransactionPoolFull(true)))

	if got := f.rawCallsFor("/get_transaction_pool"); got != 1 {
		t.Fatalf("full pool calls = %d, want 1", got)
	}

	assertGauge(t, families, "monero_transaction_pool_spent_key_images", nil, 1)
	if familyByName(families, "monero_transaction_pool_transactions_inputs") == nil {
		t.Fatal("missing per-transaction input distribution")
	}
}

func TestSlowCycleIsLogged(t *testing.T) {
	t.Parallel()

	sink := &captureSink{}
	f := newFixtureDaemon()
	// The suite pins clockNow, so advance this collector's clock to make the
	// measured cycle longer than the refresh interval.
	var ticks int
	c := newCollector(f,
		collector.WithLogger(logr.New(sink)),
		collector.WithRefreshInterval(time.Millisecond),
		collector.WithNow(func() time.Time {
			ticks++

			return time.Unix(fixedNowUnix, 0).Add(time.Duration(ticks) * time.Second)
		}),
	)
	c.Refresh(context.Background())

	if !sink.saw("exceeded refresh interval") {
		t.Fatal("expected a log when the cycle is longer than the refresh interval")
	}
}

func TestLiveCollectionDoesNotLogSlowCycle(t *testing.T) {
	t.Parallel()

	sink := &captureSink{}
	c := newCollector(newFixtureDaemon(), collector.WithLogger(logr.New(sink)))
	c.Refresh(context.Background())

	if sink.saw("exceeded refresh interval") {
		t.Fatal("live collection logged a slow cycle without a refresh interval")
	}
}

type captureSink struct {
	mu   sync.Mutex
	msgs []string
}

func (s *captureSink) Init(logr.RuntimeInfo) {}

func (s *captureSink) Enabled(int) bool { return true }

func (s *captureSink) Info(_ int, msg string, _ ...any) {
	s.mu.Lock()
	s.msgs = append(s.msgs, msg)
	s.mu.Unlock()
}

func (s *captureSink) Error(err error, msg string, _ ...any) {
	text := msg
	if err != nil {
		text = err.Error() + " " + msg
	}

	s.mu.Lock()
	s.msgs = append(s.msgs, text)
	s.mu.Unlock()
}

func (s *captureSink) WithValues(...any) logr.LogSink { return s }

func (s *captureSink) WithName(string) logr.LogSink { return s }

func (s *captureSink) saw(fragment string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, msg := range s.msgs {
		if strings.Contains(msg, fragment) {
			return true
		}
	}

	return false
}
