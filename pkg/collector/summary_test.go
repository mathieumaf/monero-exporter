package collector_test

import (
	"math"
	"testing"

	"github.com/mathieumaf/monero-exporter/pkg/collector"
)

func TestSummaryIgnoresNonFiniteValues(t *testing.T) {
	t.Parallel()

	summary := collector.NewSummary()
	summary.Insert(math.NaN())
	summary.Insert(math.Inf(1))
	summary.Insert(math.Inf(-1))

	if summary.Count() != 0 {
		t.Fatalf("non-finite samples were recorded, count=%d", summary.Count())
	}

	summary.Insert(1.5)
	if summary.Count() != 1 || summary.Sum() != 1.5 {
		t.Fatalf("count=%d sum=%v, want 1 and 1.5", summary.Count(), summary.Sum())
	}

	for _, value := range summary.Quantiles() {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			t.Fatalf("quantile %v is non-finite", value)
		}
	}
}
