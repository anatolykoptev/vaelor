package main

import (
	"slices"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/codegraph"
	"github.com/prometheus/client_golang/prometheus"
)

// TestFilterMetasByAutoIndexDirs is the RED-before-fix regression guard for
// the ephemeral-clone false-stale noise: code_graph_meta holds rows for
// WORKSPACE_DIR one-shot clones (/tmp/go-code-workspace/*) and a test
// sentinel (/test/skip/path) alongside real AUTO_INDEX_DIRS-tracked repos
// (/host/src/*). Those untracked rows are never re-queried, so their
// gocode_code_graph_age_seconds series grows forever and permanently trips
// GocodeCodeGraphStale. filterMetasByAutoIndexDirs must drop them.
//
// Falsification: revert filterMetasByAutoIndexDirs to `return metas`
// unconditionally and the "scoped to /host/src" subtest goes RED (5 keys
// returned instead of 2).
func TestFilterMetasByAutoIndexDirs(t *testing.T) {
	now := time.Now()
	metas := []codegraph.GraphMeta{
		{RepoKey: "host_src_go_code", RepoPath: "/host/src/go-code", BuiltAt: now},
		{RepoKey: "host_src_go_nerv", RepoPath: "/host/src/go-nerv", BuiltAt: now},
		// Boundary case: shares the "/host/src" string prefix but is a
		// SIBLING directory, not a subdirectory. A raw strings.HasPrefix (no
		// separator check) would wrongly include this.
		{RepoKey: "host_src_sibling", RepoPath: "/host/src-other/evil-twin", BuiltAt: now},
		{RepoKey: "workspace_livekit", RepoPath: "/tmp/go-code-workspace/livekit_client-sdk-js", BuiltAt: now},
		{RepoKey: "test_sentinel", RepoPath: "/test/skip/path", BuiltAt: now},
	}

	tests := []struct {
		name string
		dirs []string
		want []string
	}{
		{
			name: "scoped to /host/src excludes workspace clones, sentinel, and the sibling-prefix collision",
			dirs: []string{"/host/src"},
			want: []string{"host_src_go_code", "host_src_go_nerv"},
		},
		{
			name: "exact match on the repo root itself is in scope",
			dirs: []string{"/host/src/go-code"},
			want: []string{"host_src_go_code"},
		},
		{
			name: "empty AUTO_INDEX_DIRS preserves back-compat: publish everything",
			dirs: nil,
			want: []string{"host_src_go_code", "host_src_go_nerv", "host_src_sibling", "workspace_livekit", "test_sentinel"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterMetasByAutoIndexDirs(metas, tt.dirs)
			gotKeys := make([]string, len(got))
			for i, m := range got {
				gotKeys[i] = m.RepoKey
			}
			if !slices.Equal(gotKeys, tt.want) {
				t.Errorf("filterMetasByAutoIndexDirs(dirs=%v) repo keys = %v, want %v", tt.dirs, gotKeys, tt.want)
			}
		})
	}
}

// gaugeHasRepoLabel reports whether the named gauge family currently has a
// sample with label repo=repoKey. Used instead of testutil.ToFloat64 (which
// panics on a missing series) to assert ABSENCE of a series for an
// out-of-scope repo.
func gaugeHasRepoLabel(t *testing.T, family, repoKey string) bool {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != family {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, lp := range m.GetLabel() {
				if lp.GetName() == "repo" && lp.GetValue() == repoKey {
					return true
				}
			}
		}
	}
	return false
}

// TestRecordCodeGraphAges_ScopesToAutoIndexDirs is the pure-unit (no DB)
// end-to-end guard: feeds recordCodeGraphAges a synthetic []GraphMeta slice
// mixing /host/src/* rows with /tmp/go-code-workspace/* + /test/skip/path
// rows, then asserts via prometheus testutil that ONLY the /host/src/*
// repos got a gocode_code_graph_age_seconds series.
//
// Falsification: revert the filter call inside recordCodeGraphAges (record
// every meta unconditionally) and the "workspace/sentinel rows must NOT
// have a series" assertions go RED.
func TestRecordCodeGraphAges_ScopesToAutoIndexDirs(t *testing.T) {
	const family = "gocode_code_graph_age_seconds"
	now := time.Now()

	// Unique repo keys (test-scoped prefix) so this test cannot collide with
	// series set by other tests sharing the package-level codeGraphAgeSeconds
	// GaugeVec.
	inScope := "scope_test_host_src_go_code"
	outOfScopeWorkspace := "scope_test_workspace_livekit"
	outOfScopeSentinel := "scope_test_sentinel"

	metas := []codegraph.GraphMeta{
		{RepoKey: inScope, RepoPath: "/host/src/go-code", BuiltAt: now},
		{RepoKey: outOfScopeWorkspace, RepoPath: "/tmp/go-code-workspace/livekit_client-sdk-js", BuiltAt: now},
		{RepoKey: outOfScopeSentinel, RepoPath: "/test/skip/path", BuiltAt: now},
	}

	recordCodeGraphAges(metas, []string{"/host/src"})

	if !gaugeHasRepoLabel(t, family, inScope) {
		t.Errorf("%s{repo=%q} missing: an AUTO_INDEX_DIRS-tracked repo must get a series", family, inScope)
	}
	if gaugeHasRepoLabel(t, family, outOfScopeWorkspace) {
		t.Errorf("%s{repo=%q} present: a WORKSPACE_DIR one-shot clone must NOT get a series "+
			"(it is never re-queried, so it would be permanently stale noise)", family, outOfScopeWorkspace)
	}
	if gaugeHasRepoLabel(t, family, outOfScopeSentinel) {
		t.Errorf("%s{repo=%q} present: a test sentinel row outside AUTO_INDEX_DIRS must NOT get a series",
			family, outOfScopeSentinel)
	}
}
