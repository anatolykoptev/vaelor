package freshness

import (
	"os"
	"path/filepath"
	"strings"
)

// skipDirs contains directory names to skip during manifest discovery.
var skipDirs = map[string]bool{
	"vendor":       true,
	"node_modules": true,
	".git":         true,
	"testdata":     true,
}

// manifestParsers maps manifest filenames to their parsers.
var manifestParsers = map[string]func([]byte) ManifestInfo{
	"go.mod":           ParseGoMod,
	"package.json":     ParsePackageJSON,
	"pyproject.toml":   ParsePyProject,
	"requirements.txt": ParseRequirementsTxt,
	"Cargo.toml":       ParseCargoTomlFreshness,
	"pom.xml":          ParsePomXML,
	"Gemfile":          ParseGemfile,
}

// DiscoverManifests walks a directory tree and parses all recognized manifests.
func DiscoverManifests(root string) []ManifestInfo {
	var manifests []ManifestInfo

	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		parser := findParser(d.Name())
		if parser == nil {
			return nil
		}

		info := parseManifestFile(path, root, parser)
		if info != nil {
			manifests = append(manifests, *info)
		}
		return nil
	})

	return manifests
}

// findParser returns the parser for a given filename, or nil if not recognized.
func findParser(name string) func([]byte) ManifestInfo {
	if p, ok := manifestParsers[name]; ok {
		return p
	}
	// Match .csproj files by extension.
	if strings.HasSuffix(name, ".csproj") {
		return ParseCsproj
	}
	return nil
}

// parseManifestFile reads and parses a single manifest file.
func parseManifestFile(path, root string, parser func([]byte) ManifestInfo) *ManifestInfo {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	info := parser(data)

	// For Cargo.toml, enrich dependency versions from Cargo.lock if present.
	if filepath.Base(path) == "Cargo.toml" {
		enrichFromCargoLock(path, info.Dependencies)
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = path
	}
	info.ManifestPath = rel

	return &info
}

// enrichFromCargoLock reads Cargo.lock from the same directory as Cargo.toml
// and overwrites dependency versions with the resolved (exact) versions.
func enrichFromCargoLock(cargoTomlPath string, deps []Dependency) {
	lockPath := filepath.Join(filepath.Dir(cargoTomlPath), "Cargo.lock")
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		// Cargo.lock may not exist (library crates often omit it); that's fine.
		return
	}
	resolved := ParseCargoLock(lockData)
	EnrichWithCargoLock(deps, resolved)
}

// CollectDeps flattens all dependencies from multiple manifests into a single slice.
func CollectDeps(manifests []ManifestInfo) []Dependency {
	var deps []Dependency
	for _, m := range manifests {
		deps = append(deps, m.Dependencies...)
	}
	return deps
}
