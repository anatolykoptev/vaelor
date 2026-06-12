package embeddings

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetRepoInfoGauge_RegistersAndSetsOne asserts that SetRepoInfoGauge:
//  1. Is callable without panic (gauge is registered).
//  2. Emits gocode_repo_info{repo, path} = 1.0 for the given pair.
//
// RED guarantee: if SetRepoInfoGauge is deleted or the gauge declaration is
// removed, Gather finds no series with the target labels and the value
// assertion fails (require.NotNil fires).
func TestSetRepoInfoGauge_RegistersAndSetsOne(t *testing.T) {
	const (
		repo = "code_deadbeef"
		path = "/host/src/test-repo-info-metric"
	)

	// Must not panic — gauge must be registered.
	assert.NotPanics(t, func() { SetRepoInfoGauge(repo, path) })

	// Collect all metric families from the default Prometheus gatherer.
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	var found *dto.Metric
	for _, mf := range mfs {
		if mf.GetName() != "gocode_repo_info" {
			continue
		}
		for _, m := range mf.GetMetric() {
			if matchesLabels(m.GetLabel(), map[string]string{"repo": repo, "path": path}) {
				found = m
				break
			}
		}
	}

	require.NotNil(t, found,
		"gocode_repo_info{repo=%q, path=%q} must exist after SetRepoInfoGauge", repo, path)
	assert.Equal(t, 1.0, found.GetGauge().GetValue(),
		"gocode_repo_info gauge value must be 1.0 (info-style)")
}
