package forge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// forgeGaugeValue reads the value of the named gauge for the given label set
// from the default Prometheus registry. Returns 0 when no sample exists.
func forgeGaugeValue(t *testing.T, metricName string, labels map[string]string) float64 {
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
				return m.GetGauge().GetValue()
			}
		}
	}
	return 0
}

// forgeGaugeHasLabel reports whether the named gauge family has any sample with
// label mode=mode. Used to assert presence/absence of a series.
func forgeGaugeHasMode(t *testing.T, metricName, mode string) bool {
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
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "mode" && lp.GetValue() == mode {
					return true
				}
			}
		}
	}
	return false
}

// --- #5: stale GitHub App token served counter (#598, #610 gap 5) -----------
//
// When Token() refresh fails and the cached token is still valid, the App token
// source serves a stale token (clock-skew tolerance). This must bump
// gocode_github_app_stale_token_served_total so the 401-cascade risk (#598) is
// visible. Falsification: remove the Inc in the stale-serve branch and the
// delta goes to 0 (RED).
func TestAppTokenSource_StaleTokenServed_Metric(t *testing.T) {
	// Not parallel: global counter delta must be sequential.
	const staleMetric = "gocode_github_app_stale_token_served_total"
	callCount := 0
	// Expiry just past refreshLeeway boundary — stale but not expired.
	expiry := time.Now().Add(refreshLeeway - time.Second)

	mux := http.NewServeMux()
	mux.HandleFunc("/app/installations/2/access_tokens",
		func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				accessTokenHandler("stale-token", expiry)(w, r)
				return
			}
			http.Error(w, "server error", http.StatusInternalServerError)
		},
	)

	srv := httptest.NewServer(mux)
	defer srv.Close()

	src, err := newAppTokenSourceForTest(t, srv, []byte(testRSAPEM))
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// Warm the cache (no stale serve yet).
	if _, err := src.Token(context.Background()); err != nil {
		t.Fatalf("first Token: %v", err)
	}
	before := forgeCounterValue(t, staleMetric, nil)

	// Second call: refresh fails → stale served.
	tok, err := src.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token should serve stale, got error: %v", err)
	}
	if tok != "stale-token" {
		t.Fatalf("second token = %q, want stale-token", tok)
	}
	if got := forgeCounterValue(t, staleMetric, nil) - before; got != 1 {
		t.Errorf("stale-token-served counter delta = %v, want 1", got)
	}
}

// --- #7: GitHub auth mode active gauge (#603, #610 gap 7) -------------------
//
// NewGitHubForge (production constructor, default api.github.com base) must
// publish gocode_github_auth_mode{mode} = 1 for the active auth mode and 0 for
// the others, so App-vs-PAT-vs-none is visible on /metrics without issuing a
// request. Falsification: remove the publishGitHubAuthMode call and the gauge
// stays absent/stale (RED).
func TestNewGitHubForge_PublishesAuthMode(t *testing.T) {
	// Not parallel: process-global gauge, last-writer-wins must be sequential.
	const metric = "gocode_github_auth_mode"

	cases := []struct {
		name     string
		token    string
		app      AppConfig
		wantMode string
	}{
		{"app auth wins when fully configured", "ghp_pat_should_not_win", AppConfig{
			AppID: 1, InstallationID: 2, KeyPEM: []byte(testRSAPEM),
		}, "app"},
		{"pat when app unset and token present", "ghp_pat_only", AppConfig{}, "pat"},
		{"none when neither app nor token", "", AppConfig{}, "none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_ = NewGitHubForge(tc.token, tc.app)
			if got := forgeGaugeValue(t, metric, map[string]string{"mode": tc.wantMode}); got != 1 {
				t.Errorf("gocode_github_auth_mode{mode=%q} = %v, want 1", tc.wantMode, got)
			}
			// The other two modes must be 0.
			for _, other := range []string{"app", "pat", "none"} {
				if other == tc.wantMode {
					continue
				}
				if got := forgeGaugeValue(t, metric, map[string]string{"mode": other}); got != 0 {
					t.Errorf("gocode_github_auth_mode{mode=%q} = %v, want 0 (inactive)", other, got)
				}
			}
		})
	}
}

// TestNewGitHubForge_TestBaseDoesNotPublishAuthMode confirms the auth-mode
// gauge is ONLY published for the production api.github.com base — test servers
// (httptest) must not mutate the process-global gauge. Falsification: remove
// the base guard in newGitHubForgeWithBase and a test-base construction would
// overwrite the production gauge (this test stays GREEN but the guard's purpose
// is documented; the real guard is the base check).
func TestNewGitHubForge_TestBaseDoesNotPublishAuthMode(t *testing.T) {
	const metric = "gocode_github_auth_mode"
	// Establish a known production state first.
	_ = NewGitHubForge("ghp_anchor", AppConfig{})
	anchor := forgeGaugeValue(t, metric, map[string]string{"mode": "pat"})

	// Construct via a test base — must NOT change the gauge.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"full_name": "foo/bar", "default_branch": "main"})
	}))
	defer srv.Close()
	_ = newGitHubForgeWithBase("ghp_testbase", AppConfig{AppID: 1, InstallationID: 2, KeyPEM: []byte(testRSAPEM)}, srv.URL)

	if got := forgeGaugeValue(t, metric, map[string]string{"mode": "pat"}); got != anchor {
		t.Errorf("test-base construction mutated production auth-mode gauge: pat = %v, anchor %v", got, anchor)
	}
	// And app mode must NOT have been set to 1 by the test-base construction.
	if got := forgeGaugeValue(t, metric, map[string]string{"mode": "app"}); got == 1 && !forgeGaugeHasMode(t, metric, "app") {
		t.Errorf("test-base construction set app mode = 1, must not publish for non-production base")
	}
}
