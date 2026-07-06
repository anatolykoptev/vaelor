// Package envdetect performs pure-static detection of the commands a
// repository's own tooling declares (or conventionally implies) for
// install/build/test/lint. It is Phase 0 of ADR 0002
// (docs/adr/0002-environment-detect-and-verify.md): a peer of
// internal/compare / internal/analyze / internal/callgraph, importing only
// the public API of internal/freshness. It never executes a command — every
// Command produced here is inert data (an argv slice) for a caller (or a
// future, separately-gated Phase 1) to decide what, if anything, to run.
package envdetect

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/freshness"
)

// CommandSource records how a command was derived, so callers can weight
// ground-truth (manifest-declared) over convention (guessed defaults).
type CommandSource string

// Command derivation sources.
const (
	SourceManifest   CommandSource = "manifest"   // read directly from the repo's own manifest (npm "scripts", Makefile targets, poetry scripts)
	SourceConvention CommandSource = "convention" // a guessed default for the ecosystem (e.g. `go build ./...`)
)

// CommandKind classifies a candidate command by lifecycle phase.
type CommandKind string

// Command lifecycle kinds.
const (
	KindInstall CommandKind = "install"
	KindBuild   CommandKind = "build"
	KindTest    CommandKind = "test"
	KindLint    CommandKind = "lint"
)

// Command is inert data describing one candidate command as an argv slice.
// This package never executes it — no os/exec import anywhere here.
type Command struct {
	Kind    CommandKind   `json:"kind"`
	Argv    []string      `json:"argv"`
	Source  CommandSource `json:"source"`
	WorkDir string        `json:"workDir"` // relative to repo root
}

// Toolchain is the detected environment for one language layer of the repo.
type Toolchain struct {
	Language       string    `json:"language"`
	RuntimeVersion string    `json:"runtimeVersion,omitempty"`
	Manager        string    `json:"manager"` // "npm" | "yarn" | "pnpm" | "cargo" | "go" | "pip" | "poetry" | "make"
	ManifestPath   string    `json:"manifestPath"`
	WorkDir        string    `json:"workDir"`
	Commands       []Command `json:"commands"`
}

// Environment is the whole-repo detection result: one Toolchain per layer.
type Environment struct {
	Toolchains []Toolchain `json:"toolchains"`
	Polyglot   bool        `json:"polyglot"`
}

// Detect is the single public entrypoint. Pure function of on-disk state:
// no network, no execution; ctx is honoured only for early cancellation of
// the file walk (freshness.DiscoverManifests and the Makefile scan below).
func Detect(ctx context.Context, root string) (*Environment, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("envdetect: %w", err)
	}

	manifests := freshness.DiscoverManifests(root)

	toolchains, err := buildToolchains(root, manifests)
	if err != nil {
		return nil, fmt.Errorf("envdetect: build toolchains: %w", err)
	}

	toolchains, err = applyMakefiles(root, toolchains)
	if err != nil {
		return nil, fmt.Errorf("envdetect: apply makefiles: %w", err)
	}

	return &Environment{
		Toolchains: toolchains,
		Polyglot:   isPolyglot(toolchains),
	}, nil
}

// buildToolchains derives one Toolchain per manifest, merging same-directory
// Python manifests (pyproject.toml + requirements.txt describe one Python
// toolchain, not two). Manifest types with no ecosystem table row in v1
// (pom.xml, Gemfile, *.csproj) are recognized by freshness but produce no
// Toolchain here — v1 covers go/npm/cargo/python per the task scope; adding
// a 6th ecosystem is one more switch case here plus one builder function.
func buildToolchains(root string, manifests []freshness.ManifestInfo) ([]Toolchain, error) {
	var toolchains []Toolchain

	pyAccums := map[string]*pythonAccum{}
	var pyOrder []string

	for _, m := range manifests {
		dir := manifestDir(m.ManifestPath)
		base := filepath.Base(m.ManifestPath)

		switch base {
		case "go.mod":
			toolchains = append(toolchains, buildGoToolchain(m, dir))
		case "package.json":
			tc, err := buildNPMToolchain(root, dir, m)
			if err != nil {
				return nil, err
			}
			toolchains = append(toolchains, tc)
		case "Cargo.toml":
			toolchains = append(toolchains, buildCargoToolchain(m, dir))
		case "pyproject.toml", manifestRequirementsTxt:
			acc, ok := pyAccums[dir]
			if !ok {
				acc = &pythonAccum{dir: dir}
				pyAccums[dir] = acc
				pyOrder = append(pyOrder, dir)
			}
			if err := accumulatePython(acc, root, base, m); err != nil {
				return nil, err
			}
		}
	}

	for _, dir := range pyOrder {
		toolchains = append(toolchains, finalizePythonToolchain(pyAccums[dir]))
	}

	return toolchains, nil
}

// manifestDir returns the directory (relative to repo root) containing the
// given manifest path, using "." for a manifest at the repo root — matching
// the WorkDir convention used throughout this package.
func manifestDir(manifestPath string) string {
	return filepath.Dir(manifestPath)
}

// isPolyglot reports whether the detected toolchains span 2+ distinct
// non-empty languages.
func isPolyglot(toolchains []Toolchain) bool {
	languages := map[string]bool{}
	for _, tc := range toolchains {
		if tc.Language != "" {
			languages[tc.Language] = true
		}
	}
	return len(languages) >= 2
}

// fileExists reports whether path exists on disk (any error, including a
// permission error, is treated as "does not exist" — this package only ever
// uses the result to pick a default binary name, never to gate behavior).
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

const (
	managerGo     = "go"
	goAllPackages = "./..."

	// manifestRequirementsTxt is the requirements.txt basename, shared with
	// python.go's own case/Argv literals (goconst: 3+ occurrences).
	manifestRequirementsTxt = "requirements.txt"
)

// buildGoToolchain returns the go convention: freshness carries no
// per-project script convention for Go (go.mod declares no "scripts"), so
// every command is SourceConvention.
func buildGoToolchain(m freshness.ManifestInfo, dir string) Toolchain {
	return Toolchain{
		Language:       m.Language,
		RuntimeVersion: m.RuntimeVersion,
		Manager:        managerGo,
		ManifestPath:   m.ManifestPath,
		WorkDir:        dir,
		Commands: []Command{
			{Kind: KindBuild, Argv: []string{managerGo, "build", goAllPackages}, Source: SourceConvention, WorkDir: dir},
			{Kind: KindTest, Argv: []string{managerGo, "test", goAllPackages}, Source: SourceConvention, WorkDir: dir},
			{Kind: KindLint, Argv: []string{managerGo, "vet", goAllPackages}, Source: SourceConvention, WorkDir: dir},
		},
	}
}

const managerCargo = "cargo"

// buildCargoToolchain returns the cargo convention: Cargo.toml does not
// declare scripts the way package.json does, so every command is
// SourceConvention.
func buildCargoToolchain(m freshness.ManifestInfo, dir string) Toolchain {
	return Toolchain{
		Language:       m.Language,
		RuntimeVersion: m.RuntimeVersion,
		Manager:        managerCargo,
		ManifestPath:   m.ManifestPath,
		WorkDir:        dir,
		Commands: []Command{
			{Kind: KindInstall, Argv: []string{managerCargo, "fetch"}, Source: SourceConvention, WorkDir: dir},
			{Kind: KindBuild, Argv: []string{managerCargo, "build"}, Source: SourceConvention, WorkDir: dir},
			{Kind: KindTest, Argv: []string{managerCargo, "test"}, Source: SourceConvention, WorkDir: dir},
			{Kind: KindLint, Argv: []string{managerCargo, "clippy"}, Source: SourceConvention, WorkDir: dir},
		},
	}
}
