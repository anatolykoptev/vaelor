package forge

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// forgeCounterValue reads the current value of the named counter for the given
// label set from the default Prometheus registry.  Returns 0 when no sample
// has been written yet.
func forgeCounterValue(t *testing.T, metricName string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != metricName {
			continue
		}
		for _, m := range mf.GetMetric() {
			if forgeMatchLabels(m, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

func forgeMatchLabels(m *dto.Metric, want map[string]string) bool {
	have := make(map[string]string, len(m.GetLabel()))
	for _, lp := range m.GetLabel() {
		have[lp.GetName()] = lp.GetValue()
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}

const metricForgeResolve = "gocode_forge_resolve_total"

func TestForgeResolveCounter_GitHubSuccess(t *testing.T) {
	before := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "github", "outcome": "success"})
	slug, ok := ExtractSlug("https://github.com/owner/repo")
	if !ok || slug == "" {
		t.Fatalf("ExtractSlug returned unexpected failure: ok=%v slug=%q", ok, slug)
	}
	after := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "github", "outcome": "success"})
	if after-before != 1 {
		t.Errorf("github/success counter delta = %v, want 1", after-before)
	}
}

func TestForgeResolveCounter_GitLabSuccess(t *testing.T) {
	before := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "gitlab", "outcome": "success"})
	slug, ok := ExtractSlug("https://gitlab.com/group/sub/repo")
	if !ok || slug == "" {
		t.Fatalf("ExtractSlug returned unexpected failure: ok=%v slug=%q", ok, slug)
	}
	after := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "gitlab", "outcome": "success"})
	if after-before != 1 {
		t.Errorf("gitlab/success counter delta = %v, want 1", after-before)
	}
}

func TestForgeResolveCounter_UnknownHostReject(t *testing.T) {
	before := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "unknown", "outcome": "reject_unknown_host"})
	_, ok := ExtractSlug("git@evil.com:owner/repo.git")
	if ok {
		t.Fatal("ExtractSlug should reject unknown SSH host")
	}
	after := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "unknown", "outcome": "reject_unknown_host"})
	if after-before != 1 {
		t.Errorf("unknown/reject_unknown_host counter delta = %v, want 1", after-before)
	}
}

func TestForgeResolveCounter_InvalidForm(t *testing.T) {
	before := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "unknown", "outcome": "invalid_form"})
	_, ok := ExtractSlug("")
	if ok {
		t.Fatal("ExtractSlug should reject empty input")
	}
	after := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "unknown", "outcome": "invalid_form"})
	if after-before != 1 {
		t.Errorf("unknown/invalid_form counter delta = %v, want 1", after-before)
	}
}

func TestForgeResolveCounter_GitHubSSHSuccess(t *testing.T) {
	before := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "github", "outcome": "success"})
	slug, ok := ExtractSlug("git@github.com:owner/repo.git")
	if !ok || slug == "" {
		t.Fatalf("ExtractSlug SSH form failed: ok=%v slug=%q", ok, slug)
	}
	after := forgeCounterValue(t, metricForgeResolve, map[string]string{"forge": "github", "outcome": "success"})
	if after-before != 1 {
		t.Errorf("github/success (SSH) counter delta = %v, want 1", after-before)
	}
}
