package goanalysis_test

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// gatherCounterSum returns the sum of all samples for the named metric
// family where all provided label key→value pairs match. A nil/empty
// matchLabels matches every sample of the family — the right shape for a
// plain (unlabelled) prometheus.Counter like
// gocode_goanalysis_func_value_alias_edges_total.
//
// Mirrors internal/callgraph/metrics_test.go's gatherCounterSum (same-shape
// helper, duplicated per test package because the metric it targets here —
// funcValueAliasEdgesTotal — is unexported and this is an external
// goanalysis_test package; querying prometheus.DefaultGatherer by metric
// NAME works across the package boundary without needing the symbol).
func gatherCounterSum(t *testing.T, metricName string, matchLabels map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	var total float64
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			matched := true
			for _, lp := range m.GetLabel() {
				if want, ok := matchLabels[lp.GetName()]; ok && lp.GetValue() != want {
					matched = false
					break
				}
			}
			if matched {
				if c := m.GetCounter(); c != nil {
					total += c.GetValue()
				}
			}
		}
	}
	return total
}
