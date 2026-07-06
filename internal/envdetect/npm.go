package envdetect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/anatolykoptev/go-code/internal/freshness"
)

// npm/yarn/pnpm binary names, selected by lockfile presence in the manifest's
// own directory.
const (
	managerNPM  = "npm"
	managerYarn = "yarn"
	managerPNPM = "pnpm"
)

// npmScriptKinds enumerates, in a fixed order, the package.json "scripts"
// keys this package recognizes as ground truth. Each key is exactly the
// string value of its Command kind (e.g. "build" == string(KindBuild)),
// which is npm's own convention too.
var npmScriptKinds = []CommandKind{KindBuild, KindTest, KindLint}

type npmPackageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

// buildNPMToolchain re-reads package.json (freshness.ManifestInfo does not
// carry "scripts") for ground-truth build/test/lint commands. Install is
// always emitted as SourceConvention: it is not itself a "scripts" entry —
// `npm install` (or the yarn/pnpm equivalent) runs implicitly. A recognized
// key with no matching script is never fabricated (no command is better than
// a wrong guess).
func buildNPMToolchain(root, dir string, m freshness.ManifestInfo) (Toolchain, error) {
	manifestAbs := filepath.Join(root, m.ManifestPath)
	data, err := os.ReadFile(manifestAbs)
	if err != nil {
		return Toolchain{}, fmt.Errorf("envdetect: read %s: %w", m.ManifestPath, err)
	}

	var pkg npmPackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return Toolchain{}, fmt.Errorf("envdetect: parse %s: %w", m.ManifestPath, err)
	}

	manager := detectNPMManager(filepath.Join(root, dir))

	commands := []Command{
		{Kind: KindInstall, Argv: []string{manager, "install"}, Source: SourceConvention, WorkDir: dir},
	}
	for _, kind := range npmScriptKinds {
		key := string(kind)
		script, ok := pkg.Scripts[key]
		if !ok || script == "" {
			continue
		}
		commands = append(commands, Command{
			Kind:    kind,
			Argv:    []string{manager, "run", key},
			Source:  SourceManifest,
			WorkDir: dir,
		})
	}

	return Toolchain{
		Language:       m.Language,
		RuntimeVersion: m.RuntimeVersion,
		Manager:        manager,
		ManifestPath:   m.ManifestPath,
		WorkDir:        dir,
		Commands:       commands,
	}, nil
}

// detectNPMManager picks the package manager binary by lockfile presence in
// absDir (the directory containing package.json), defaulting to npm when no
// lockfile is present.
func detectNPMManager(absDir string) string {
	switch {
	case fileExists(filepath.Join(absDir, "yarn.lock")):
		return managerYarn
	case fileExists(filepath.Join(absDir, "pnpm-lock.yaml")):
		return managerPNPM
	default:
		return managerNPM
	}
}
