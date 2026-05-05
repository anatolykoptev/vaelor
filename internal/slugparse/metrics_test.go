package slugparse

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// counterValue reads the current value of the named counter metric for the
// given label pairs from the default Prometheus registry.  Returns 0 when no
// sample has been written yet.
func counterValue(t *testing.T, metricName string, labels map[string]string) float64 {
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
			if matchLabels(m, labels) {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}

// matchLabels reports whether all key=value pairs in want are present in m's
// label set.
func matchLabels(m *dto.Metric, want map[string]string) bool {
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

const metricSlugNormalize = "gocode_slug_normalize_total"

func TestSlugNormalizeCounter_BareAccept(t *testing.T) {
	before := counterValue(t, metricSlugNormalize, map[string]string{"form": "bare", "kind": "accept"})
	_, err := Parse("owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := counterValue(t, metricSlugNormalize, map[string]string{"form": "bare", "kind": "accept"})
	if after-before != 1 {
		t.Errorf("bare/accept counter delta = %v, want 1", after-before)
	}
}

func TestSlugNormalizeCounter_GitHubURLAccept(t *testing.T) {
	before := counterValue(t, metricSlugNormalize, map[string]string{"form": "github_url", "kind": "accept"})
	_, err := Parse("https://github.com/owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := counterValue(t, metricSlugNormalize, map[string]string{"form": "github_url", "kind": "accept"})
	if after-before != 1 {
		t.Errorf("github_url/accept counter delta = %v, want 1", after-before)
	}
}

func TestSlugNormalizeCounter_GitLabURLAccept(t *testing.T) {
	before := counterValue(t, metricSlugNormalize, map[string]string{"form": "gitlab_url", "kind": "accept"})
	_, err := Parse("https://gitlab.com/owner/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := counterValue(t, metricSlugNormalize, map[string]string{"form": "gitlab_url", "kind": "accept"})
	if after-before != 1 {
		t.Errorf("gitlab_url/accept counter delta = %v, want 1", after-before)
	}
}

func TestSlugNormalizeCounter_GitHubSSHAccept(t *testing.T) {
	before := counterValue(t, metricSlugNormalize, map[string]string{"form": "github_ssh", "kind": "accept"})
	_, err := Parse("git@github.com:owner/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := counterValue(t, metricSlugNormalize, map[string]string{"form": "github_ssh", "kind": "accept"})
	if after-before != 1 {
		t.Errorf("github_ssh/accept counter delta = %v, want 1", after-before)
	}
}

func TestSlugNormalizeCounter_GitLabSSHAccept(t *testing.T) {
	before := counterValue(t, metricSlugNormalize, map[string]string{"form": "gitlab_ssh", "kind": "accept"})
	_, err := Parse("git@gitlab.com:owner/repo.git")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	after := counterValue(t, metricSlugNormalize, map[string]string{"form": "gitlab_ssh", "kind": "accept"})
	if after-before != 1 {
		t.Errorf("gitlab_ssh/accept counter delta = %v, want 1", after-before)
	}
}

func TestSlugNormalizeCounter_InvalidReject(t *testing.T) {
	before := counterValue(t, metricSlugNormalize, map[string]string{"form": "invalid", "kind": "reject"})
	_, err := Parse("")
	if err == nil {
		t.Fatal("expected error for empty input")
	}
	after := counterValue(t, metricSlugNormalize, map[string]string{"form": "invalid", "kind": "reject"})
	if after-before != 1 {
		t.Errorf("invalid/reject counter delta = %v, want 1", after-before)
	}
}

func TestSlugNormalizeCounter_BareRejectTooManySegments(t *testing.T) {
	before := counterValue(t, metricSlugNormalize, map[string]string{"form": "bare", "kind": "reject"})
	// Three segments is invalid when AllowSubgroups=false (the default).
	_, err := Parse("owner/repo/extra")
	if err == nil {
		t.Fatal("expected error for three-segment bare slug")
	}
	after := counterValue(t, metricSlugNormalize, map[string]string{"form": "bare", "kind": "reject"})
	if after-before != 1 {
		t.Errorf("bare/reject counter delta = %v, want 1", after-before)
	}
}
