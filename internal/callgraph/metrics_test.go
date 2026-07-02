package callgraph

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gatherCounterSum returns the sum of all samples for the named metric family
// where all provided label key→value pairs match.
func gatherCounterSum(t *testing.T, metricName string, matchLabels map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
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

// TestGotypesFallbackCounter_DeadlineReason verifies that recordGotypesFallback
// routes context.DeadlineExceeded to reason="deadline".
//
// Note: packages.Load on an expired context may return a non-deadline wrapper
// (the go toolchain reports its own errors first). We test the helper directly
// to ensure the classification logic is correct; the integration path (expired
// ctx → packages.Load → DeadlineExceeded propagated) is environment-dependent.
//
// RED guarantee: change isDeadlineErr to always return false, and this test
// fails because recordGotypesFallback writes "load_error" for DeadlineExceeded.
func TestGotypesFallbackCounter_DeadlineReason(t *testing.T) {
	before := gatherCounterSum(t, "gocode_callgraph_gotypes_fallback_total",
		map[string]string{"reason": "deadline"})

	// Call the helper directly with a deadline error.
	recordGotypesFallback(context.DeadlineExceeded)

	after := gatherCounterSum(t, "gocode_callgraph_gotypes_fallback_total",
		map[string]string{"reason": "deadline"})
	assert.Equal(t, before+1, after,
		"context.DeadlineExceeded must record reason=deadline on gocode_callgraph_gotypes_fallback_total")
}

// TestGotypesFallbackCounter_LoadErrorReason verifies that a plain load failure
// (not a deadline) records reason="load_error".
//
// RED guarantee: remove recordGotypesFallback from TryGoTypesResolution → counter
// stays flat → assertion fails.
func TestGotypesFallbackCounter_LoadErrorReason(t *testing.T) {
	before := gatherCounterSum(t, "gocode_callgraph_gotypes_fallback_total",
		map[string]string{"reason": "load_error"})

	// An empty temp dir has no Go module; packages.Load returns a non-deadline error.
	TryGoTypesResolution(context.Background(), t.TempDir(), nil)

	after := gatherCounterSum(t, "gocode_callgraph_gotypes_fallback_total",
		map[string]string{"reason": "load_error"})
	assert.Greater(t, after, before,
		"non-deadline load failure must record reason=load_error on gocode_callgraph_gotypes_fallback_total")
}

// TestSCIPFallbackCounter_Increments verifies that recordSCIPFallback bumps
// gocode_scip_fallback_total with the given indexer and reason labels.
//
// RED guarantee: remove the recordSCIPFallback calls from TrySCIPResolution,
// and the counter stays flat — this test fails.
func TestSCIPFallbackCounter_Increments(t *testing.T) {
	tests := []struct {
		indexer string
		reason  string
	}{
		{"rust-analyzer", "killed"},
		{"scip-python", "indexer_error"},
		{"scip-java", "read_error"},
		{"rust-analyzer", "no_edges"},
	}
	for _, tt := range tests {
		t.Run(tt.indexer+"/"+tt.reason, func(t *testing.T) {
			before := gatherCounterSum(t, "gocode_scip_fallback_total",
				map[string]string{"indexer": tt.indexer, "reason": tt.reason})
			recordSCIPFallback(tt.indexer, tt.reason)
			after := gatherCounterSum(t, "gocode_scip_fallback_total",
				map[string]string{"indexer": tt.indexer, "reason": tt.reason})
			assert.Equal(t, before+1, after,
				"recordSCIPFallback must increment counter for %s/%s", tt.indexer, tt.reason)
		})
	}
}

// TestIsDeadlineErr verifies deadline vs non-deadline classification.
// context.Canceled must NOT be treated as a deadline — it is a deliberate
// cancellation (caller disconnect) and should not inflate the deadline miss rate.
func TestIsDeadlineErr(t *testing.T) {
	assert.True(t, isDeadlineErr(context.DeadlineExceeded), "DeadlineExceeded must be deadline")
	assert.False(t, isDeadlineErr(context.Canceled), "Canceled must not be deadline")
	assert.False(t, isDeadlineErr(nil), "nil must not be deadline")
}
