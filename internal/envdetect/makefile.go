package envdetect

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	makefileName = "Makefile"
	managerMake  = "make"
)

// makefileSkipDirs mirrors internal/freshness/discover.go's skipDirs list
// (unexported there, so not reusable directly) — kept intentionally small
// and specific to this one extra walk rather than importing freshness
// internals.
var makefileSkipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"target":       true,
}

// targetNameRe matches a bare Makefile target name ("name" in "name:" or
// "name: deps"). Go's RE2-based regexp has no negative lookahead, so
// variable-assignment exclusion ("name := value" / "name ?= value") is done
// separately in parseMakefileTargets by checking the character right after
// the matched colon.
var targetNameRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)

// makefileTargetKinds maps recognized Makefile target names to Command
// kinds. Three of the four match a CommandKind string exactly (Make/repo
// convention already uses those names); "check" has no direct Kind in the
// ADR's four-kind enum, so by long-standing Make/autotools convention
// (`make check` runs the test suite) it is treated as an alias for KindTest
// — a Decision-1 deviation, called out in the PR description.
var makefileTargetKinds = map[string]CommandKind{
	string(KindInstall): KindInstall,
	string(KindBuild):   KindBuild,
	string(KindTest):    KindTest,
	string(KindLint):    KindLint,
	"check":             KindTest,
}

// applyMakefiles overlays Makefile-declared targets onto the toolchains
// already detected for their directory. A Makefile target is
// SourceManifest — explicit human intent — so for any CommandKind it covers,
// it replaces a same-directory SourceConvention guess of that kind (and is
// simply added alongside any existing SourceManifest command of that kind,
// e.g. an npm "test" script). A directory with a Makefile but no other
// detected manifest gets its own standalone "make" Toolchain.
func applyMakefiles(root string, toolchains []Toolchain) ([]Toolchain, error) {
	makefiles, err := discoverMakefiles(root)
	if err != nil {
		return nil, err
	}

	dirs := make([]string, 0, len(makefiles))
	for dir := range makefiles {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for _, dir := range dirs {
		targets, err := parseMakefileTargets(makefiles[dir])
		if err != nil {
			return nil, err
		}
		if len(targets) == 0 {
			continue
		}

		matched := false
		for i := range toolchains {
			if toolchains[i].WorkDir != dir {
				continue
			}
			matched = true
			applyMakefileOverrides(&toolchains[i], dir, targets)
		}
		if !matched {
			toolchains = append(toolchains, standaloneMakeToolchain(dir, targets))
		}
	}

	return toolchains, nil
}

// discoverMakefiles walks root for files named Makefile, returning a map of
// directory (relative to root, "." for the repo root) to absolute path.
func discoverMakefiles(root string) (map[string]string, error) {
	found := map[string]string{}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort walk, matches freshness.DiscoverManifests
		}
		if d.IsDir() {
			if makefileSkipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != makefileName {
			return nil
		}

		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return nil //nolint:nilerr // unresolvable relative path — skip this Makefile
		}
		found[rel] = path
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("envdetect: walk for Makefile: %w", err)
	}

	return found, nil
}

// parseMakefileTargets scans a Makefile for the first occurrence of each
// recognized target name, returning the CommandKind -> target name mapping.
func parseMakefileTargets(path string) (map[CommandKind]string, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from our own directory walk, not user input
	if err != nil {
		return nil, fmt.Errorf("envdetect: open %s: %w", path, err)
	}
	defer f.Close()

	targets := map[CommandKind]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		name, ok := parseMakefileTargetLine(scanner.Text())
		if !ok {
			continue
		}
		kind, recognized := makefileTargetKinds[name]
		if !recognized {
			continue
		}
		if _, exists := targets[kind]; !exists {
			targets[kind] = name
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("envdetect: scan %s: %w", path, err)
	}

	return targets, nil
}

// parseMakefileTargetLine extracts a target name from a Makefile rule
// header line ("name:" or "name: deps"), rejecting recipe lines (leading
// tab) and variable assignments ("name := value" / "name ?= value" — an '='
// immediately follows the matched ':').
func parseMakefileTargetLine(line string) (string, bool) {
	if strings.HasPrefix(line, "\t") {
		return "", false // recipe line, not a rule header
	}

	name, rest, found := strings.Cut(line, ":")
	if !found {
		return "", false
	}
	if strings.HasPrefix(rest, "=") {
		return "", false // "name:= value" — variable assignment, not a rule
	}

	name = strings.TrimSpace(name)
	if !targetNameRe.MatchString(name) {
		return "", false
	}
	return name, true
}

// makefileCommandOrder is the fixed iteration order used whenever emitting
// Makefile-sourced commands, so output is deterministic regardless of Go's
// map iteration order.
var makefileCommandOrder = []CommandKind{KindInstall, KindBuild, KindTest, KindLint}

// applyMakefileOverrides drops any existing SourceConvention command of a
// kind the Makefile also covers, then appends the Makefile-sourced command
// for that kind.
func applyMakefileOverrides(tc *Toolchain, dir string, targets map[CommandKind]string) {
	for _, kind := range makefileCommandOrder {
		name, ok := targets[kind]
		if !ok {
			continue
		}

		filtered := make([]Command, 0, len(tc.Commands))
		for _, cmd := range tc.Commands {
			if cmd.Kind == kind && cmd.Source == SourceConvention {
				continue
			}
			filtered = append(filtered, cmd)
		}
		filtered = append(filtered, Command{
			Kind:    kind,
			Argv:    []string{managerMake, name},
			Source:  SourceManifest,
			WorkDir: dir,
		})
		tc.Commands = filtered
	}
}

// standaloneMakeToolchain builds a Toolchain for a directory whose only
// recognized manifest is a Makefile.
func standaloneMakeToolchain(dir string, targets map[CommandKind]string) Toolchain {
	tc := Toolchain{
		Manager:      managerMake,
		ManifestPath: filepath.Join(dir, makefileName),
		WorkDir:      dir,
	}
	for _, kind := range makefileCommandOrder {
		name, ok := targets[kind]
		if !ok {
			continue
		}
		tc.Commands = append(tc.Commands, Command{
			Kind:    kind,
			Argv:    []string{managerMake, name},
			Source:  SourceManifest,
			WorkDir: dir,
		})
	}
	return tc
}
