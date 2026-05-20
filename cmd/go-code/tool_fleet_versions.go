package main

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/fleet"
	"github.com/anatolykoptev/go-code/internal/fleet/docker"
	"github.com/anatolykoptev/go-code/internal/fleet/ssh"
	"github.com/anatolykoptev/go-code/internal/polyglot/pinned"
	mcpserver "github.com/anatolykoptev/go-mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// FleetVersionsInput is the input schema for the fleet_versions tool.
type FleetVersionsInput struct {
	Repo    string `json:"repo,omitempty"    jsonschema_description:"Path or GitHub URL to the indexed repo. Pinned image versions are extracted from its Dockerfile and docker-compose*.yml files. Empty = runtime probe only."`
	Host    string `json:"host,omitempty"    jsonschema_description:"Probe target. Empty or 'local://' = local docker socket (default, no config required); 'ssh://[user@]host[:port]' = remote via system ssh (uses ~/.ssh/config; requires GOCODE_FLEET_SSH_ENABLE=true). 'docker://' aliases to local."`
	Service string `json:"service,omitempty" jsonschema_description:"Optional filter: matches container name first, then com.docker.compose.service label. Pass empty to list all containers."`
}

// FleetVersionsOutput is the JSON-serialised response for fleet_versions.
type FleetVersionsOutput struct {
	Targets  []TargetReport `json:"targets"`
	Warnings []string       `json:"warnings,omitempty"`
}

// TargetReport is one probe target's result within FleetVersionsOutput.
type TargetReport struct {
	Target string            `json:"target"` // canonical target string as supplied by the caller
	Diffs  []fleet.ImageDiff `json:"diffs"`
	Error  string            `json:"error,omitempty"` // per-target soft-fail; probe errors land here, not at tool level
}

// buildFleetRegistry is the package-level seam for tests. Production code builds
// the registry from config; tests swap this var to inject fake probes.
//
// Always registers docker (canonical scheme) and ssh (driver-level gate via
// WithEnabled — returns ErrSSHDisabled immediately when FleetSSHEnable is false).
// This ensures callers see a descriptive error ("driver disabled; set
// GOCODE_FLEET_SSH_ENABLE=true") rather than a cryptic "no probe registered".
var buildFleetRegistry = func(cfg Config) *fleet.Registry {
	reg := fleet.NewRegistry()
	reg.Register(docker.New(
		docker.WithSocketPath(cfg.FleetDockerSocket),
		docker.WithTimeout(cfg.FleetTimeout),
	))
	reg.Register(ssh.New(
		ssh.WithEnabled(cfg.FleetSSHEnable),
		ssh.WithBinary(cfg.FleetSSHBinary),
		ssh.WithTimeout(cfg.FleetTimeout),
	))
	return reg
}

// registerFleetVersions registers the fleet_versions MCP tool on server.
// The tool is always registered regardless of config; the handler degrades
// gracefully when no probes are reachable.
func registerFleetVersions(server *mcp.Server, cfg Config, deps analyze.Deps) {
	mcpserver.AddTool(server, &mcp.Tool{
		Name: "fleet_versions",
		Description: "Compare pinned image versions in a repo (Dockerfile, docker-compose*.yml) " +
			"against images currently running on a host. " +
			"Default host = local docker socket — no configuration needed for the common case. " +
			"For remote hosts use 'ssh://[user@]host[:port]' (requires GOCODE_FLEET_SSH_ENABLE=true; " +
			"uses the system ssh binary so ~/.ssh/config is the single source of truth for ProxyJump, " +
			"identity, port, and known_hosts). " +
			"Service filter: container name is checked first, then com.docker.compose.service label. " +
			"Pass the most specific identifier available (container name preferred). " +
			"Soft failures (unreachable host, missing repo) appear in the JSON as target-level errors, " +
			"not tool-level errors, so partial results are still returned.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input FleetVersionsInput) (*mcp.CallToolResult, error) {
		return fleetVersionsHandler(ctx, cfg, deps, input)
	})
}

// fleetVersionsHandler is the testable core of the fleet_versions handler.
// Extracted so tests can call it directly with injected cfg/deps.
func fleetVersionsHandler(ctx context.Context, cfg Config, deps analyze.Deps, input FleetVersionsInput) (*mcp.CallToolResult, error) {
	// 1. Validate service filter early (fast-fail; drivers do their own check too).
	if err := validateFleetServiceFilter(input.Service); err != nil {
		return errResult(fmt.Sprintf("invalid service filter: %s", err)), nil
	}

	// 2. Parse host target. Empty → "local://".
	hostStr := input.Host
	if hostStr == "" {
		hostStr = "local://"
	}

	t, err := fleet.ParseTarget(hostStr)
	if err != nil {
		return errResult(fmt.Sprintf("invalid host: %s", err)), nil
	}

	// 3. Option A alias: "local" → "docker" for registry dispatch.
	//    Target.Raw is preserved for the report's Target field.
	if t.Scheme == "local" {
		t.Scheme = "docker"
	}

	// 4. Resolve repo and collect pinned images (optional; best-effort).
	var pinnedImgs []pinned.PinnedImage
	var repoWarn string
	if input.Repo != "" {
		root, cleanup, resolveErr := resolveRoot(ctx, input.Repo, "", deps)
		if resolveErr != nil {
			// Warn but continue — probe still runs against empty pinned set.
			repoWarn = fmt.Sprintf("repo resolve: %s", resolveErr)
		} else {
			defer cleanup()
			pinnedImgs, _ = pinned.Collect(root) // best-effort; ignore error per spec
		}
	}

	// 5. Build (or get injected) registry.
	reg := buildFleetRegistry(cfg)

	// 6. Get probe for this target.
	probe, probeErr := reg.Get(t)

	// 7. Call probe.
	var runtimeImgs []fleet.RuntimeImage
	var targetErr string

	if probeErr != nil {
		targetErr = probeErr.Error()
	} else {
		runtimeImgs, err = probe.List(ctx, t, fleet.Filter{Service: input.Service})
		if err != nil {
			targetErr = err.Error()
		}
	}

	// If probe failed, runtimeImgs stays nil → Diff produces OnlySource rows
	// for whatever pinnedImgs we have.

	// 8. Diff.
	diffs := fleet.Diff(pinnedImgs, runtimeImgs)

	// 9. Assemble output.
	report := TargetReport{
		Target: hostStr,
		Diffs:  diffs,
	}
	if targetErr != "" {
		report.Error = targetErr
	}
	if repoWarn != "" && targetErr == "" {
		// Stash repo warning in per-target error (target itself was fine).
		report.Error = repoWarn
	}

	out := FleetVersionsOutput{
		Targets: []TargetReport{report},
	}

	// Top-level warning when there is genuinely nothing to show.
	if len(pinnedImgs) == 0 && len(runtimeImgs) == 0 && targetErr == "" {
		out.Warnings = append(out.Warnings,
			"no pinned images parsed and no runtime containers found; check that the repo contains a Dockerfile or docker-compose*.yml and that the docker socket is accessible")
	}
	if repoWarn != "" {
		out.Warnings = append(out.Warnings, repoWarn)
	}

	data, marshalErr := json.MarshalIndent(out, "", "  ")
	if marshalErr != nil {
		return errResult(fmt.Sprintf("marshal: %s", marshalErr)), nil
	}
	return textResult(string(data)), nil
}

// validateFleetServiceFilter rejects service names containing characters
// outside [a-zA-Z0-9._-]. Called at handler entry for fast-fail UX.
// Drivers do their own check; this layer ensures the error is a tool-level
// errResult rather than a per-target soft-fail.
func validateFleetServiceFilter(service string) error {
	if service == "" {
		return nil
	}
	for _, r := range service {
		if !isFleetFilterChar(r) {
			return fmt.Errorf("service name %q contains invalid character %q", service, r)
		}
	}
	return nil
}

// isFleetFilterChar reports whether r is in [a-zA-Z0-9._-].
func isFleetFilterChar(r rune) bool {
	if unicode.IsLetter(r) && r <= 0x7F {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	return r == '.' || r == '_' || r == '-'
}
