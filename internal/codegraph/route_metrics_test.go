package codegraph

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// readCounter reads the current float64 value of a prometheus.Counter
// by writing its internal state into a dto.Metric.  This avoids the
// testutil package which is not vendored in this repo.
func readCounter(t *testing.T, c prometheus.Counter) float64 {
	t.Helper()
	var m dto.Metric
	if err := c.Write(&m); err != nil {
		t.Fatalf("counter.Write: %v", err)
	}
	return m.GetCounter().GetValue()
}

// TestRouteMetrics_RecordHelpers calls each of the five record helpers once
// with sample labels and asserts the backing CounterVec incremented by 1.
// This is the CG-T1 registration smoke test for the route->graph observability
// scaffold.  The test is white-box (package codegraph) so it can reference the
// package-level counter vars directly alongside the record* helpers.
func TestRouteMetrics_RecordHelpers(t *testing.T) {
	t.Run("routesExtracted", func(t *testing.T) {
		c := routesExtractedTotal.WithLabelValues("repo1", "gin", "server")
		before := readCounter(t, c)
		recordRoutesExtracted("repo1", "gin", "server")
		after := readCounter(t, c)
		if after-before != 1 {
			t.Errorf("routesExtractedTotal: want +1, got +%.0f", after-before)
		}
	})

	t.Run("routeEdgeBuilt", func(t *testing.T) {
		c := routeEdgesBuiltTotal.WithLabelValues("repo1", "HANDLES")
		before := readCounter(t, c)
		recordRouteEdgeBuilt("repo1", "HANDLES")
		after := readCounter(t, c)
		if after-before != 1 {
			t.Errorf("routeEdgesBuiltTotal: want +1, got +%.0f", after-before)
		}
	})

	t.Run("routeHandlerUnresolved", func(t *testing.T) {
		c := routeHandlerUnresolvedTotal.WithLabelValues("repo1")
		before := readCounter(t, c)
		recordRouteHandlerUnresolved("repo1")
		after := readCounter(t, c)
		if after-before != 1 {
			t.Errorf("routeHandlerUnresolvedTotal: want +1, got +%.0f", after-before)
		}
	})

	t.Run("routeRejected", func(t *testing.T) {
		c := routeRejectedTotal.WithLabelValues("repo1", "junk")
		before := readCounter(t, c)
		recordRouteRejected("repo1", "junk")
		after := readCounter(t, c)
		if after-before != 1 {
			t.Errorf("routeRejectedTotal: want +1, got +%.0f", after-before)
		}
	})

	t.Run("graphBuild", func(t *testing.T) {
		c := graphBuildTotal.WithLabelValues("repo1", "ok")
		before := readCounter(t, c)
		recordGraphBuild("repo1", "ok")
		after := readCounter(t, c)
		if after-before != 1 {
			t.Errorf("graphBuildTotal: want +1, got +%.0f", after-before)
		}
	})
}
