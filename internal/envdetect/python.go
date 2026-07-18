package envdetect

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/freshness"
)

const (
	managerPip    = "pip"
	managerPoetry = "poetry"
	argInstall    = "install"
)

// pythonScriptKinds enumerates, in a fixed order, the [tool.poetry.scripts] /
// [project.scripts] entry names this package recognizes as ground truth.
// Each name is exactly the string value of its Command kind.
var pythonScriptKinds = []CommandKind{KindBuild, KindTest, KindLint}

// pythonAccum gathers ground truth across every manifest found in one
// directory (pyproject.toml and requirements.txt describe a single Python
// toolchain, not two, when both live in the same directory).
type pythonAccum struct {
	dir              string
	manifestPath     string
	runtimeVersion   string
	hasPyproject     bool
	hasRequirements  bool
	hasPoetrySection bool
	scriptNames      map[string]bool // set of declared script/entry-point names matching a recognized kind
}

// accumulatePython folds one manifest (pyproject.toml or requirements.txt)
// found in a directory into acc.
func accumulatePython(acc *pythonAccum, root, base string, m freshness.ManifestInfo) error {
	if acc.runtimeVersion == "" {
		acc.runtimeVersion = m.RuntimeVersion
	}

	switch base {
	case "pyproject.toml":
		acc.hasPyproject = true
		acc.manifestPath = m.ManifestPath

		data, err := os.ReadFile(filepath.Join(root, m.ManifestPath))
		if err != nil {
			return fmt.Errorf("envdetect: read %s: %w", m.ManifestPath, err)
		}
		parsePyprojectScripts(string(data), acc)
	case manifestRequirementsTxt:
		acc.hasRequirements = true
		if acc.manifestPath == "" {
			acc.manifestPath = m.ManifestPath
		}
	}
	return nil
}

// parsePyprojectScripts scans pyproject.toml content (line-based, matching
// the hand-rolled TOML scanning style already used by
// internal/freshness/parse_python.go — no TOML library dependency) for a
// [tool.poetry] section (any presence marks the project as poetry-managed,
// used to pick the install command) and [tool.poetry.scripts] /
// [project.scripts] entries matching a recognized kind name.
func parsePyprojectScripts(data string, acc *pythonAccum) {
	var section string
	for line := range strings.SplitSeq(data, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.TrimSpace(strings.Trim(trimmed, "[]"))
			if section == "tool.poetry" || strings.HasPrefix(section, "tool.poetry.") {
				acc.hasPoetrySection = true
			}
			continue
		}

		if section != "tool.poetry.scripts" && section != "project.scripts" {
			continue
		}

		key, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		name := strings.TrimSpace(key)
		if !isRecognizedPythonScript(name) {
			continue
		}
		if acc.scriptNames == nil {
			acc.scriptNames = map[string]bool{}
		}
		acc.scriptNames[name] = true
	}
}

func isRecognizedPythonScript(name string) bool {
	for _, kind := range pythonScriptKinds {
		if string(kind) == name {
			return true
		}
	}
	return false
}

// finalizePythonToolchain builds the Toolchain for one directory's
// accumulated Python ground truth, falling back to convention defaults
// (pip/poetry install, pytest) exactly where no ground truth exists.
func finalizePythonToolchain(acc *pythonAccum) Toolchain {
	manager := managerPip
	if acc.hasPoetrySection {
		manager = managerPoetry
	}

	var commands []Command
	commands = append(commands, pythonInstallCommands(acc)...)

	hasTestScript := false
	for _, kind := range pythonScriptKinds {
		name := string(kind)
		if !acc.scriptNames[name] {
			continue
		}
		if kind == KindTest {
			hasTestScript = true
		}
		argv := []string{name}
		if acc.hasPoetrySection {
			argv = []string{managerPoetry, "run", name}
		}
		commands = append(commands, Command{Kind: kind, Argv: argv, Source: SourceManifest, WorkDir: acc.dir})
	}

	if !hasTestScript {
		commands = append(commands, Command{Kind: KindTest, Argv: []string{"pytest"}, Source: SourceConvention, WorkDir: acc.dir})
	}

	return Toolchain{
		Language:       "python",
		RuntimeVersion: acc.runtimeVersion,
		Manager:        manager,
		ManifestPath:   acc.manifestPath,
		WorkDir:        acc.dir,
		Commands:       commands,
	}
}

// pythonInstallCommands emits an install command per install-capable
// manifest present, in a fixed dir/pyproject order: `pip install -r
// requirements.txt` when requirements.txt exists, and `poetry install` (if a
// [tool.poetry] section was found) or `pip install .` (otherwise) when
// pyproject.toml exists.
func pythonInstallCommands(acc *pythonAccum) []Command {
	var commands []Command
	if acc.hasRequirements {
		commands = append(commands, Command{
			Kind:    KindInstall,
			Argv:    []string{managerPip, argInstall, "-r", manifestRequirementsTxt},
			Source:  SourceConvention,
			WorkDir: acc.dir,
		})
	}
	if acc.hasPyproject {
		argv := []string{managerPip, argInstall, "."}
		if acc.hasPoetrySection {
			argv = []string{managerPoetry, argInstall}
		}
		commands = append(commands, Command{Kind: KindInstall, Argv: argv, Source: SourceConvention, WorkDir: acc.dir})
	}
	return commands
}
