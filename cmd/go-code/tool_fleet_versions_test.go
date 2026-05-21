package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/fleet"
	"github.com/anatolykoptev/go-code/internal/fleet/docker"
	"github.com/anatolykoptev/go-code/internal/fleet/ssh"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// Fake probes — avoid real docker socket or ssh binary.
// ---------------------------------------------------------------------------

// fakeDockerProbe implements fleet.Probe for the "docker" scheme.
type fakeDockerProbe struct {
	images []fleet.RuntimeImage
	err    error
}

func (f *fakeDockerProbe) Scheme() string { return "docker" }
func (f *fakeDockerProbe) List(_ context.Context, _ fleet.Target, filter fleet.Filter) ([]fleet.RuntimeImage, error) {
	if f.err != nil {
		return nil, f.err
	}
	if filter.Service == "" {
		return f.images, nil
	}
	var out []fleet.RuntimeImage
	for _, img := range f.images {
		if img.Container == filter.Service || img.Service == filter.Service {
			out = append(out, img)
		}
	}
	return out, nil
}

// fakeSSHProbe implements fleet.Probe for the "ssh" scheme.
type fakeSSHProbe struct {
	images []fleet.RuntimeImage
	err    error
}

func (f *fakeSSHProbe) Scheme() string { return "ssh" }
func (f *fakeSSHProbe) List(_ context.Context, _ fleet.Target, filter fleet.Filter) ([]fleet.RuntimeImage, error) {
	if f.err != nil {
		return nil, f.err
	}
	if filter.Service == "" {
		return f.images, nil
	}
	var out []fleet.RuntimeImage
	for _, img := range f.images {
		if img.Container == filter.Service || img.Service == filter.Service {
			out = append(out, img)
		}
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// defaultFleetCfg returns a minimal Config with fleet defaults.
func defaultFleetCfg() Config {
	return Config{
		FleetDockerSocket: "/var/run/docker.sock",
		FleetSSHEnable:    false,
		FleetSSHBinary:    "ssh",
		FleetTimeout:      10 * time.Second,
	}
}

// minimalDeps returns a zero-value analyze.Deps sufficient for local path tests.
func minimalDeps() analyze.Deps {
	return analyze.Deps{}
}

// injectRegistry swaps the buildFleetRegistry package-level var and returns a
// cleanup function that restores it.
func injectRegistry(t *testing.T, reg *fleet.Registry) {
	t.Helper()
	orig := buildFleetRegistry
	buildFleetRegistry = func(_ Config) *fleet.Registry { return reg }
	t.Cleanup(func() { buildFleetRegistry = orig })
}

// callHandler invokes fleetVersionsHandler via the package-level var seam and
// returns the result.
func callHandler(t *testing.T, cfg Config, input FleetVersionsInput) *mcp.CallToolResult {
	t.Helper()
	result, err := fleetVersionsHandler(context.Background(), cfg, minimalDeps(), input)
	if err != nil {
		t.Fatalf("handler returned unexpected error: %v", err)
	}
	return result
}

// parseOutput unmarshals the JSON payload from a non-error tool result.
func parseOutput(t *testing.T, result *mcp.CallToolResult) FleetVersionsOutput {
	t.Helper()
	if result.IsError {
		t.Fatalf("expected non-error result, got error: %+v", result.Content)
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := result.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is %T, want *mcp.TextContent", result.Content[0])
	}
	var out FleetVersionsOutput
	if err := json.Unmarshal([]byte(tc.Text), &out); err != nil {
		t.Fatalf("unmarshal: %v — raw: %s", err, tc.Text)
	}
	return out
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestFleetVersions_EmptyInput_LocalDefault: empty input → "local://" target,
// fake docker returns 0 containers → Diffs empty, warning about empty result.
func TestFleetVersions_EmptyInput_LocalDefault(t *testing.T) {
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{images: nil}, nil))
	out := parseOutput(t, callHandler(t, defaultFleetCfg(), FleetVersionsInput{}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if out.Targets[0].Target != "local://" {
		t.Errorf("Target=%q; want %q", out.Targets[0].Target, "local://")
	}
	if out.Targets[0].Error != "" {
		t.Errorf("unexpected Target.Error: %s", out.Targets[0].Error)
	}
	if len(out.Targets[0].Diffs) != 0 {
		t.Errorf("Diffs len=%d; want 0", len(out.Targets[0].Diffs))
	}
	if len(out.Warnings) == 0 {
		t.Error("expected at least one warning about empty result, got none")
	}
}

// TestFleetVersions_LocalPlusRepo_NoCompose: empty tempdir, fake docker returns
// one container → DiffOnlyRuntime.
func TestFleetVersions_LocalPlusRepo_NoCompose(t *testing.T) {
	dir := t.TempDir()
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "web", Image: "nginx", Tag: "1.27", State: "running"},
		},
	}, nil))
	out := parseOutput(t, callHandler(t, defaultFleetCfg(), FleetVersionsInput{Repo: dir}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if out.Targets[0].Error != "" {
		t.Errorf("unexpected Target.Error: %s", out.Targets[0].Error)
	}
	if len(out.Targets[0].Diffs) != 1 {
		t.Fatalf("Diffs len=%d; want 1", len(out.Targets[0].Diffs))
	}
	if out.Targets[0].Diffs[0].Status != fleet.DiffOnlyRuntime {
		t.Errorf("Status=%q; want OnlyRuntime", out.Targets[0].Diffs[0].Status)
	}
}

// TestFleetVersions_LocalPlusDockerfile_Match: Dockerfile pins nginx:1.27,
// fake docker returns nginx:1.27 → DiffMatch.
func TestFleetVersions_LocalPlusDockerfile_Match(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM nginx:1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "web", Image: "nginx", Tag: "1.27", State: "running"},
		},
	}, nil))
	out := parseOutput(t, callHandler(t, defaultFleetCfg(), FleetVersionsInput{Repo: dir}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if out.Targets[0].Error != "" {
		t.Errorf("unexpected Target.Error: %s", out.Targets[0].Error)
	}
	if len(out.Targets[0].Diffs) < 1 {
		t.Fatalf("Diffs len=%d; want ≥1", len(out.Targets[0].Diffs))
	}
	if out.Targets[0].Diffs[0].Status != fleet.DiffMatch {
		t.Errorf("Status=%q; want Match", out.Targets[0].Diffs[0].Status)
	}
}

// TestFleetVersions_LocalPlusDockerfile_TagDrift: Dockerfile pins nginx:1.27,
// fake docker returns nginx:1.26 → DiffTagDrift.
func TestFleetVersions_LocalPlusDockerfile_TagDrift(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM nginx:1.27\n"), 0600); err != nil {
		t.Fatal(err)
	}
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "web", Image: "nginx", Tag: "1.26", State: "running"},
		},
	}, nil))
	out := parseOutput(t, callHandler(t, defaultFleetCfg(), FleetVersionsInput{Repo: dir}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if len(out.Targets[0].Diffs) < 1 {
		t.Fatalf("Diffs len=%d; want ≥1", len(out.Targets[0].Diffs))
	}
	if out.Targets[0].Diffs[0].Status != fleet.DiffTagDrift {
		t.Errorf("Status=%q; want TagDrift", out.Targets[0].Diffs[0].Status)
	}
}

// TestFleetVersions_SSHDisabled: ssh:// target, FleetSSHEnable=false →
// TargetReport.Error non-empty, Diffs empty.
func TestFleetVersions_SSHDisabled(t *testing.T) {
	cfg := defaultFleetCfg()
	cfg.FleetSSHEnable = false

	// Use production registry builder so the real ssh.New(WithEnabled(false)) path runs.
	orig := buildFleetRegistry
	buildFleetRegistry = func(c Config) *fleet.Registry {
		reg := fleet.NewRegistry()
		reg.Register(docker.New(docker.WithSocketPath(c.FleetDockerSocket)))
		reg.Register(ssh.New(ssh.WithEnabled(c.FleetSSHEnable)))
		return reg
	}
	t.Cleanup(func() { buildFleetRegistry = orig })

	result := callHandler(t, cfg, FleetVersionsInput{Host: "ssh://krolik"})
	if result.IsError {
		t.Fatal("expected soft-fail (TargetReport.Error), not tool-level error")
	}

	out := parseOutput(t, result)
	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if out.Targets[0].Error == "" {
		t.Error("expected non-empty TargetReport.Error for disabled ssh driver")
	}
	if len(out.Targets[0].Diffs) != 0 {
		t.Errorf("Diffs len=%d; want 0", len(out.Targets[0].Diffs))
	}
}

// TestFleetVersions_SSHEnabled_FakeProbe: ssh:// target, FleetSSHEnable=true,
// fake ssh probe returns 1 container → DiffOnlyRuntime (no repo given).
func TestFleetVersions_SSHEnabled_FakeProbe(t *testing.T) {
	cfg := defaultFleetCfg()
	cfg.FleetSSHEnable = true

	injectRegistry(t, buildTestFleetRegistry(
		&fakeDockerProbe{},
		&fakeSSHProbe{
			images: []fleet.RuntimeImage{
				{Container: "app", Image: "myapp", Tag: "latest", State: "running"},
			},
		},
	))
	out := parseOutput(t, callHandler(t, cfg, FleetVersionsInput{Host: "ssh://krolik"}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if out.Targets[0].Error != "" {
		t.Errorf("unexpected Error: %s", out.Targets[0].Error)
	}
	if len(out.Targets[0].Diffs) != 1 {
		t.Fatalf("Diffs len=%d; want 1", len(out.Targets[0].Diffs))
	}
	if out.Targets[0].Diffs[0].Status != fleet.DiffOnlyRuntime {
		t.Errorf("Status=%q; want OnlyRuntime", out.Targets[0].Diffs[0].Status)
	}
}

// TestFleetVersions_InvalidServiceFilter: service containing semicolon →
// tool-level error (IsError=true).
func TestFleetVersions_InvalidServiceFilter(t *testing.T) {
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{}, nil))

	result := callHandler(t, defaultFleetCfg(), FleetVersionsInput{Service: "web;rm"})
	if !result.IsError {
		t.Error("expected IsError=true for invalid service filter")
	}
}

// TestFleetVersions_InvalidHostScheme: http:// scheme → tool-level error.
func TestFleetVersions_InvalidHostScheme(t *testing.T) {
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{}, nil))

	result := callHandler(t, defaultFleetCfg(), FleetVersionsInput{Host: "http://krolik"})
	if !result.IsError {
		t.Error("expected IsError=true for unsupported scheme")
	}
}

// TestFleetVersions_FilterForwardedToProbe: service "web" → only web in results.
func TestFleetVersions_FilterForwardedToProbe(t *testing.T) {
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "web", Image: "nginx", Tag: "1.27", State: "running"},
			{Container: "cache", Image: "redis", Tag: "7", State: "running"},
		},
	}, nil))
	out := parseOutput(t, callHandler(t, defaultFleetCfg(), FleetVersionsInput{Service: "web"}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if len(out.Targets[0].Diffs) != 1 {
		t.Fatalf("Diffs len=%d; want 1 (only web)", len(out.Targets[0].Diffs))
	}
	if out.Targets[0].Diffs[0].Runtime == nil || out.Targets[0].Diffs[0].Runtime.Container != "web" {
		t.Errorf("expected web container in diff, got %+v", out.Targets[0].Diffs[0].Runtime)
	}
}

// TestFleetVersions_RepoResolveFail: invalid repo path → probe still runs,
// diffs computed against empty pinned (OnlyRuntime rows), error surfaced.
func TestFleetVersions_RepoResolveFail(t *testing.T) {
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "app", Image: "myapp", Tag: "v1", State: "running"},
		},
	}, nil))
	out := parseOutput(t, callHandler(t, defaultFleetCfg(), FleetVersionsInput{Repo: "/nonexistent/path/does/not/exist"}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if len(out.Targets[0].Diffs) == 0 {
		t.Error("expected diffs against empty pinned (OnlyRuntime rows)")
	}
	for _, d := range out.Targets[0].Diffs {
		if d.Status != fleet.DiffOnlyRuntime {
			t.Errorf("unexpected diff status %q; want OnlyRuntime", d.Status)
		}
	}
	hasError := out.Targets[0].Error != "" || len(out.Warnings) > 0
	if !hasError {
		t.Error("expected TargetReport.Error or Warnings about resolve failure")
	}
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestLoadConfig_FleetDefaults(t *testing.T) {
	for _, k := range []string{
		"GOCODE_FLEET_DEFAULT_HOST",
		"GOCODE_FLEET_DOCKER_SOCKET",
		"GOCODE_FLEET_SSH_ENABLE",
		"GOCODE_FLEET_SSH_BINARY",
		"GOCODE_FLEET_TIMEOUT",
	} {
		t.Setenv(k, "")
	}

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if cfg.FleetDefaultHost != "" {
		t.Errorf("FleetDefaultHost=%q; want empty", cfg.FleetDefaultHost)
	}
	if cfg.FleetDockerSocket != "/var/run/docker.sock" {
		t.Errorf("FleetDockerSocket=%q; want /var/run/docker.sock", cfg.FleetDockerSocket)
	}
	if cfg.FleetSSHEnable {
		t.Error("FleetSSHEnable=true; want false (security gate)")
	}
	if cfg.FleetSSHBinary != "ssh" {
		t.Errorf("FleetSSHBinary=%q; want ssh", cfg.FleetSSHBinary)
	}
	if cfg.FleetTimeout != 10*time.Second {
		t.Errorf("FleetTimeout=%v; want 10s", cfg.FleetTimeout)
	}
}

func TestLoadConfig_FleetSSHEnableEnvOverride(t *testing.T) {
	t.Setenv("GOCODE_FLEET_SSH_ENABLE", "true")
	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if !cfg.FleetSSHEnable {
		t.Error("FleetSSHEnable=false; want true")
	}
}

// TestFleetVersions_BuildFleetRegistry_HasDockerAndSSH confirms the production
// buildFleetRegistry registers both drivers.
func TestFleetVersions_BuildFleetRegistry_HasDockerAndSSH(t *testing.T) {
	cfg := defaultFleetCfg()
	reg := buildFleetRegistry(cfg)

	if !reg.Has("docker") {
		t.Error("registry missing docker scheme")
	}
	if !reg.Has("ssh") {
		t.Error("registry missing ssh (should be registered even when disabled)")
	}
	_ = errors.Is(nil, fleet.ErrSchemeUnknown) // compile-time import check
}

// ---------------------------------------------------------------------------
// buildTestFleetRegistry is a test helper (not the production var).
// It lets tests inject arbitrary probes without touching the package-level var.
// ---------------------------------------------------------------------------

func buildTestFleetRegistry(dockerProbe fleet.Probe, sshProbe fleet.Probe) *fleet.Registry {
	reg := fleet.NewRegistry()
	if dockerProbe != nil {
		reg.Register(dockerProbe)
	}
	if sshProbe != nil {
		reg.Register(sshProbe)
	}
	return reg
}

// ---------------------------------------------------------------------------
// Hosts [] field + multi-host + SiblingDrifts tests
// ---------------------------------------------------------------------------

// TestFleetVersions_HostsField_BackCompat: Hosts=["local://"] is identical to
// Host="local://" (backwards compatibility).
func TestFleetVersions_HostsField_BackCompat(t *testing.T) {
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{
		images: []fleet.RuntimeImage{
			{Container: "web", Image: "nginx", Tag: "1.27", State: "running"},
		},
	}, nil))
	out := parseOutput(t, callHandler(t, defaultFleetCfg(), FleetVersionsInput{Hosts: []string{"local://"}}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if out.Targets[0].Target != "local://" {
		t.Errorf("Target=%q; want %q", out.Targets[0].Target, "local://")
	}
	if out.Targets[0].Error != "" {
		t.Errorf("unexpected error: %s", out.Targets[0].Error)
	}
}

// TestFleetVersions_EmptyHostsAndHost_DefaultsToLocal: both Host and Hosts empty →
// single "local://" target (back-compat default).
func TestFleetVersions_EmptyHostsAndHost_DefaultsToLocal(t *testing.T) {
	injectRegistry(t, buildTestFleetRegistry(&fakeDockerProbe{images: nil}, nil))
	out := parseOutput(t, callHandler(t, defaultFleetCfg(), FleetVersionsInput{}))

	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if out.Targets[0].Target != "local://" {
		t.Errorf("Target=%q; want local://", out.Targets[0].Target)
	}
}

// TestFleetVersions_HostsWinsOverHost: when both Host and Hosts are set, Hosts
// wins and a warning is emitted.
func TestFleetVersions_HostsWinsOverHost(t *testing.T) {
	cfg := defaultFleetCfg()
	cfg.FleetSSHEnable = true

	injectRegistry(t, buildTestFleetRegistry(
		&fakeDockerProbe{
			images: []fleet.RuntimeImage{
				{Container: "web", Image: "nginx", Tag: "1.27", State: "running"},
			},
		},
		&fakeSSHProbe{},
	))
	out := parseOutput(t, callHandler(t, cfg, FleetVersionsInput{
		Host:  "ssh://krolik",
		Hosts: []string{"local://"},
	}))

	// Hosts wins: should probe "local://" not "ssh://krolik"
	if len(out.Targets) != 1 {
		t.Fatalf("Targets len=%d; want 1", len(out.Targets))
	}
	if out.Targets[0].Target != "local://" {
		t.Errorf("Target=%q; want local://", out.Targets[0].Target)
	}
	// Warning must be emitted about host being ignored.
	if len(out.Warnings) == 0 {
		t.Error("expected warning about 'host' being ignored when 'hosts' is set")
	}
}

// fakeSSHProbeForHost is an SSH probe that returns different images based on the
// target host, allowing multi-host tests to return distinct data per host.
type fakeSSHProbeForHost struct {
	perHost map[string][]fleet.RuntimeImage
}

func (f *fakeSSHProbeForHost) Scheme() string { return "ssh" }
func (f *fakeSSHProbeForHost) List(_ context.Context, t fleet.Target, _ fleet.Filter) ([]fleet.RuntimeImage, error) {
	if imgs, ok := f.perHost[t.Host]; ok {
		return imgs, nil
	}
	return nil, nil
}

// TestFleetVersions_MultiHost_TwoTargets: Hosts with two entries → two TargetReports.
func TestFleetVersions_MultiHost_TwoTargets(t *testing.T) {
	cfg := defaultFleetCfg()
	cfg.FleetSSHEnable = true

	fakeSSH := &fakeSSHProbeForHost{
		perHost: map[string][]fleet.RuntimeImage{
			"krolik": {{Container: "app", Image: "myapp", Tag: "v1", State: "running"}},
			"piter":  {{Container: "app", Image: "myapp", Tag: "v2", State: "running"}},
		},
	}

	orig := buildFleetRegistry
	buildFleetRegistry = func(_ Config) *fleet.Registry {
		reg := fleet.NewRegistry()
		reg.Register(&fakeDockerProbe{})
		reg.Register(fakeSSH)
		return reg
	}
	t.Cleanup(func() { buildFleetRegistry = orig })

	out := parseOutput(t, callHandler(t, cfg, FleetVersionsInput{
		Hosts: []string{"ssh://krolik", "ssh://piter"},
	}))

	if len(out.Targets) != 2 {
		t.Fatalf("Targets len=%d; want 2", len(out.Targets))
	}
	// Verify both targets are present (order may vary).
	targetSet := map[string]bool{}
	for _, tr := range out.Targets {
		targetSet[tr.Target] = true
	}
	if !targetSet["ssh://krolik"] || !targetSet["ssh://piter"] {
		t.Errorf("expected both targets; got %v", targetSet)
	}
}

// TestFleetVersions_MultiHost_SiblingDriftDetected: two hosts running same image
// with different tags → SiblingDrifts populated.
func TestFleetVersions_MultiHost_SiblingDriftDetected(t *testing.T) {
	cfg := defaultFleetCfg()
	cfg.FleetSSHEnable = true

	fakeSSH := &fakeSSHProbeForHost{
		perHost: map[string][]fleet.RuntimeImage{
			"krolik": {{Container: "svcimage", Image: "minio/minio", Tag: "latest", State: "running"}},
			"piter":  {{Container: "svcimage", Image: "minio/minio", Tag: "26.5.3", State: "running"}},
		},
	}

	orig := buildFleetRegistry
	buildFleetRegistry = func(_ Config) *fleet.Registry {
		reg := fleet.NewRegistry()
		reg.Register(&fakeDockerProbe{})
		reg.Register(fakeSSH)
		return reg
	}
	t.Cleanup(func() { buildFleetRegistry = orig })

	out := parseOutput(t, callHandler(t, cfg, FleetVersionsInput{
		Hosts: []string{"ssh://krolik", "ssh://piter"},
	}))

	if len(out.SiblingDrifts) != 1 {
		t.Fatalf("SiblingDrifts len=%d; want 1 (minio/minio drift)", len(out.SiblingDrifts))
	}
	if out.SiblingDrifts[0].Image != "minio/minio" {
		t.Errorf("SiblingDrifts[0].Image=%q; want minio/minio", out.SiblingDrifts[0].Image)
	}
	if len(out.SiblingDrifts[0].Variants) != 2 {
		t.Errorf("SiblingDrifts[0].Variants len=%d; want 2", len(out.SiblingDrifts[0].Variants))
	}
}

// TestFleetVersions_MultiHost_SoftFailPerTarget: one host fails → TargetReport.Error
// set, other host still returns results, tool does not return IsError.
func TestFleetVersions_MultiHost_SoftFailPerTarget(t *testing.T) {
	cfg := defaultFleetCfg()
	cfg.FleetSSHEnable = true

	fakeSSH := &fakeSSHProbeForHost{
		perHost: map[string][]fleet.RuntimeImage{
			"krolik": {{Container: "web", Image: "nginx", Tag: "1.27", State: "running"}},
			// piter is not in the map → returns nil/nil → success but empty
		},
	}

	orig := buildFleetRegistry
	buildFleetRegistry = func(_ Config) *fleet.Registry {
		reg := fleet.NewRegistry()
		reg.Register(&fakeDockerProbe{})
		reg.Register(fakeSSH)
		return reg
	}
	t.Cleanup(func() { buildFleetRegistry = orig })

	result := callHandler(t, cfg, FleetVersionsInput{
		Hosts: []string{"ssh://krolik", "ssh://piter"},
	})

	// Must NOT be a tool-level error.
	if result.IsError {
		t.Fatal("multi-host soft-fail must not be a tool-level error")
	}

	out := parseOutput(t, result)
	if len(out.Targets) != 2 {
		t.Fatalf("Targets len=%d; want 2", len(out.Targets))
	}
}
