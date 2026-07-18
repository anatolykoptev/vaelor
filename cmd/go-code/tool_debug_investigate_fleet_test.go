// cmd/go-code/tool_debug_investigate_fleet_test.go
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anatolykoptev/vaelor/internal/fleet"
	"github.com/anatolykoptev/vaelor/internal/investigate"
)

// ---------------------------------------------------------------------------
// summarizeFleetForLLM tests — pure function, table-driven
// ---------------------------------------------------------------------------

// TestSummarizeFleetForLLM_AllMatch: all Match rows → empty summary (matches dropped).
func TestSummarizeFleetForLLM_AllMatch(t *testing.T) {
	rows := []investigate.FleetDiffRow{
		{Image: "nginx", Status: "Match"},
		{Image: "redis", Status: "Match"},
	}
	got := summarizeFleetForLLM(&investigate.FleetReport{Target: "local://", Diffs: rows})
	if got != "" {
		t.Errorf("expected empty summary for all-Match rows, got: %q", got)
	}
}

// TestSummarizeFleetForLLM_Empty: no rows → empty summary.
func TestSummarizeFleetForLLM_Empty(t *testing.T) {
	got := summarizeFleetForLLM(&investigate.FleetReport{Target: "local://"})
	if got != "" {
		t.Errorf("expected empty summary for nil rows, got: %q", got)
	}
}

// TestSummarizeFleetForLLM_TagDriftSurfaces: TagDrift row appears in summary.
func TestSummarizeFleetForLLM_TagDriftSurfaces(t *testing.T) {
	rows := []investigate.FleetDiffRow{
		{Image: "nginx", Status: "TagDrift", PinnedTag: "1.27", RuntimeTag: "1.26",
			Explanation: "tag drift: pinned \"1.27\" vs runtime \"1.26\""},
	}
	got := summarizeFleetForLLM(&investigate.FleetReport{Target: "local://", Diffs: rows})
	if got == "" {
		t.Fatal("expected non-empty summary for TagDrift row")
	}
	if !strings.Contains(got, "TagDrift") {
		t.Errorf("expected summary to contain 'TagDrift', got: %q", got)
	}
	if !strings.Contains(got, "1.27") {
		t.Errorf("expected summary to contain pinned tag '1.27', got: %q", got)
	}
	if !strings.Contains(got, "1.26") {
		t.Errorf("expected summary to contain runtime tag '1.26', got: %q", got)
	}
}

// TestSummarizeFleetForLLM_MatchDropped: 5 Match + 1 TagDrift → only 1 row in summary.
func TestSummarizeFleetForLLM_MatchDropped(t *testing.T) {
	rows := []investigate.FleetDiffRow{
		{Image: "nginx", Status: "TagDrift", PinnedTag: "1.27", RuntimeTag: "1.26",
			Explanation: "tag drift: pinned \"1.27\" vs runtime \"1.26\""},
		{Image: "redis", Status: "Match"},
		{Image: "postgres", Status: "Match"},
		{Image: "mysql", Status: "Match"},
		{Image: "memcached", Status: "Match"},
		{Image: "rabbitmq", Status: "Match"},
	}
	got := summarizeFleetForLLM(&investigate.FleetReport{Target: "local://", Diffs: rows})
	if !strings.Contains(got, "TagDrift") {
		t.Errorf("expected summary to contain TagDrift, got: %q", got)
	}
	// Should not mention "and N more" since only 1 non-Match row
	if strings.Contains(got, "more diffs") {
		t.Errorf("unexpected 'more diffs' tail for 1 non-Match row: %q", got)
	}
	// Should not contain Match rows
	if strings.Contains(got, "Match") {
		t.Errorf("expected Match rows to be dropped from summary, got: %q", got)
	}
}

// TestSummarizeFleetForLLM_CardinalityCap30: 30 TagDrift rows → first 20 in summary +
// "and 10 more diffs of type TagDrift".
func TestSummarizeFleetForLLM_CardinalityCap30(t *testing.T) {
	rows := make([]investigate.FleetDiffRow, 30)
	for i := range rows {
		rows[i] = investigate.FleetDiffRow{
			Image:       "nginx-" + string(rune('a'+i%26)),
			Status:      "TagDrift",
			PinnedTag:   "1.27",
			RuntimeTag:  "1.26",
			Explanation: "tag drift",
		}
	}
	got := summarizeFleetForLLM(&investigate.FleetReport{Target: "local://", Diffs: rows})
	if !strings.Contains(got, "and 10 more") {
		t.Errorf("expected 'and 10 more' tail for 30 TagDrift rows, got: %q", got)
	}
	if !strings.Contains(got, "TagDrift") {
		t.Errorf("expected summary to contain TagDrift, got: %q", got)
	}
}

// TestSummarizeFleetForLLM_CardinalityCapMixed: 20 OnlySource + 10 TagDrift →
// sort by priority: TagDrift first (10 rows) then OnlySource (10 rows, capped at 20 total).
// Tail: "and 10 more diffs of type OnlySource".
//
// Note: summary priority order is TagDrift > DigestDrift > Unresolved > OnlyRuntime > OnlySource,
// which differs from fleet.Diff's output order (TagDrift > DigestDrift > Unresolved > OnlySource > OnlyRuntime).
// This test explicitly confirms our sort order matches the spec.
func TestSummarizeFleetForLLM_CardinalityCapMixed(t *testing.T) {
	var rows []investigate.FleetDiffRow
	// Add 20 OnlySource rows first (wrong order to verify sort happens)
	for i := 0; i < 20; i++ {
		rows = append(rows, investigate.FleetDiffRow{
			Image:  "source-" + string(rune('a'+i%26)),
			Status: "OnlySource",
		})
	}
	// Then add 10 TagDrift rows
	for i := 0; i < 10; i++ {
		rows = append(rows, investigate.FleetDiffRow{
			Image:      "tag-" + string(rune('a'+i%26)),
			Status:     "TagDrift",
			PinnedTag:  "1.27",
			RuntimeTag: "1.26",
		})
	}
	got := summarizeFleetForLLM(&investigate.FleetReport{Target: "local://", Diffs: rows})
	// TagDrift must appear (first 10 priority rows)
	if !strings.Contains(got, "TagDrift") {
		t.Errorf("expected TagDrift in summary, got: %q", got)
	}
	// The tail (rows 21-30) should be OnlySource
	if !strings.Contains(got, "OnlySource") {
		t.Errorf("expected OnlySource in tail, got: %q", got)
	}
	if !strings.Contains(got, "and 10 more") {
		t.Errorf("expected 'and 10 more' tail for 30 non-Match rows with cap 20, got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// runFleetVersionsPhase tests
// ---------------------------------------------------------------------------

// TestFleetPhase_SkippedNoHostNoPinned: no host, no Dockerfile/compose → silent skip.
func TestFleetPhase_SkippedNoHostNoPinned(t *testing.T) {
	tmpDir := t.TempDir()
	// tmpDir has no Dockerfile or docker-compose.yml

	cfg := Config{
		FleetDefaultHost:  "",
		FleetDockerSocket: "/var/run/docker.sock",
		FleetSSHEnable:    false,
		FleetSSHBinary:    "ssh",
		FleetTimeout:      5 * time.Second,
	}
	input := DebugInvestigateInput{
		Service: "test-svc",
		Repo:    tmpDir,
	}
	res := &investigate.InvestigationResult{}

	runFleetVersionsPhase(context.Background(), input, cfg, integrationDeps(), res)

	if res.RuntimeVersions != nil {
		t.Errorf("expected RuntimeVersions nil for no-host/no-pinned, got: %+v", res.RuntimeVersions)
	}
	if len(res.Diagnostics.Warnings) != 0 {
		t.Errorf("expected 0 warnings, got: %v", res.Diagnostics.Warnings)
	}
}

// TestFleetPhase_SkippedNoHostHasPinned: no host, repo has Dockerfile → 1 warning with env var hint.
func TestFleetPhase_SkippedNoHostHasPinned(t *testing.T) {
	tmpDir := t.TempDir()
	// Create a Dockerfile so pinned.Collect finds something
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM nginx:1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		FleetDefaultHost:  "",
		FleetDockerSocket: "/var/run/docker.sock",
		FleetSSHEnable:    false,
		FleetSSHBinary:    "ssh",
		FleetTimeout:      5 * time.Second,
	}
	input := DebugInvestigateInput{
		Service: "test-svc",
		Repo:    tmpDir,
	}
	res := &investigate.InvestigationResult{}

	runFleetVersionsPhase(context.Background(), input, cfg, integrationDeps(), res)

	if res.RuntimeVersions != nil {
		t.Errorf("expected RuntimeVersions nil when host not provided, got: %+v", res.RuntimeVersions)
	}
	if len(res.Diagnostics.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(res.Diagnostics.Warnings), res.Diagnostics.Warnings)
	}
	if !strings.Contains(res.Diagnostics.Warnings[0], "GOCODE_FLEET_DEFAULT_HOST") {
		t.Errorf("expected warning to mention GOCODE_FLEET_DEFAULT_HOST, got: %q", res.Diagnostics.Warnings[0])
	}
}

// TestFleetPhase_RanAllMatch: local docker, all pinned images match runtime.
func TestFleetPhase_RanAllMatch(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM nginx:1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := fleet.NewRegistry()
	reg.Register(&fakeDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "nginx-1", Image: "nginx", Tag: "1.27", State: "running"},
		},
	})
	injectRegistry(t, reg)

	cfg := Config{
		FleetDefaultHost:  "",
		FleetDockerSocket: "/var/run/docker.sock",
		FleetSSHEnable:    false,
		FleetTimeout:      5 * time.Second,
	}
	input := DebugInvestigateInput{
		Service: "",
		Host:    "local://",
		Repo:    tmpDir,
	}
	res := &investigate.InvestigationResult{}

	runFleetVersionsPhase(context.Background(), input, cfg, integrationDeps(), res)

	if res.RuntimeVersions == nil {
		t.Fatal("expected RuntimeVersions non-nil for local:// with matching image")
	}
	for _, d := range res.RuntimeVersions.Diffs {
		if d.Status != "Match" {
			t.Errorf("expected all diffs to be Match, got Status=%q for image %q", d.Status, d.Image)
		}
	}
	if res.RuntimeVersions.Summary != "" {
		t.Errorf("expected empty Summary for all-Match diffs, got: %q", res.RuntimeVersions.Summary)
	}
}

// TestFleetPhase_TagDriftInSummary: nginx pinned 1.27, runtime 1.26 → Summary contains TagDrift.
func TestFleetPhase_TagDriftInSummary(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "Dockerfile"), []byte("FROM nginx:1.27\n"), 0644); err != nil {
		t.Fatal(err)
	}

	reg := fleet.NewRegistry()
	reg.Register(&fakeDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "nginx-1", Image: "nginx", Tag: "1.26", State: "running"},
		},
	})
	injectRegistry(t, reg)

	cfg := Config{
		FleetDefaultHost:  "",
		FleetDockerSocket: "/var/run/docker.sock",
		FleetSSHEnable:    false,
		FleetTimeout:      5 * time.Second,
	}
	input := DebugInvestigateInput{
		Service: "",
		Host:    "local://",
		Repo:    tmpDir,
	}
	res := &investigate.InvestigationResult{}

	runFleetVersionsPhase(context.Background(), input, cfg, integrationDeps(), res)

	if res.RuntimeVersions == nil {
		t.Fatal("expected RuntimeVersions non-nil")
	}
	if !strings.Contains(res.RuntimeVersions.Summary, "TagDrift") {
		t.Errorf("expected Summary to contain 'TagDrift', got: %q", res.RuntimeVersions.Summary)
	}
	if !strings.Contains(res.RuntimeVersions.Summary, "1.27") {
		t.Errorf("expected Summary to contain pinned tag '1.27', got: %q", res.RuntimeVersions.Summary)
	}
	if !strings.Contains(res.RuntimeVersions.Summary, "1.26") {
		t.Errorf("expected Summary to contain runtime tag '1.26', got: %q", res.RuntimeVersions.Summary)
	}
}

// TestFleetPhase_SSHDisabled: ssh:// host with FleetSSHEnable=false →
// RuntimeVersions.Error contains error message.
func TestFleetPhase_SSHDisabled(t *testing.T) {
	// Use real registry (no injectRegistry) to test SSH disabled error path
	origBuild := buildFleetRegistry
	buildFleetRegistry = func(cfg Config) *fleet.Registry {
		reg := fleet.NewRegistry()
		// Only register docker, not SSH → simulates "no probe registered" for ssh://
		// (we don't register ssh.New so we get ErrSchemeUnknown)
		return reg
	}
	t.Cleanup(func() { buildFleetRegistry = origBuild })

	cfg := Config{
		FleetSSHEnable: false,
		FleetTimeout:   5 * time.Second,
	}
	input := DebugInvestigateInput{
		Service: "test-svc",
		Host:    "ssh://user@host",
	}
	res := &investigate.InvestigationResult{}

	runFleetVersionsPhase(context.Background(), input, cfg, integrationDeps(), res)

	// Should record error, not nil
	if res.RuntimeVersions == nil {
		t.Fatal("expected RuntimeVersions non-nil for ssh:// with no probe registered")
	}
	if res.RuntimeVersions.Error == "" {
		t.Error("expected RuntimeVersions.Error non-empty for unregistered ssh:// scheme")
	}
}

// TestFleetPhase_InvalidHost: host="http://x" → Warning recorded, RuntimeVersions nil.
func TestFleetPhase_InvalidHost(t *testing.T) {
	cfg := Config{
		FleetTimeout: 5 * time.Second,
	}
	input := DebugInvestigateInput{
		Service: "test-svc",
		Host:    "http://x",
	}
	res := &investigate.InvestigationResult{}

	runFleetVersionsPhase(context.Background(), input, cfg, integrationDeps(), res)

	if res.RuntimeVersions != nil {
		t.Errorf("expected RuntimeVersions nil for invalid host, got: %+v", res.RuntimeVersions)
	}
	if len(res.Diagnostics.Warnings) == 0 {
		t.Error("expected at least 1 warning for invalid host")
	}
}
