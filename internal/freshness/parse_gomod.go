package freshness

import (
	"strings"
)

// ParseGoMod extracts dependencies and Go version from go.mod content.
func ParseGoMod(data []byte) ManifestInfo {
	info := ManifestInfo{Language: "go"}
	inRequire := false
	for line := range strings.SplitSeq(string(data), "\n") {
		inRequire = processGoModLine(strings.TrimSpace(line), inRequire, &info)
	}
	return info
}

// processGoModLine handles a single go.mod line, returns updated inRequire state.
func processGoModLine(trimmed string, inRequire bool, info *ManifestInfo) bool {
	if trimmed == "" || strings.HasPrefix(trimmed, "//") {
		return inRequire
	}

	if strings.HasPrefix(trimmed, "go ") && !inRequire {
		info.RuntimeVersion = strings.TrimSpace(strings.TrimPrefix(trimmed, "go"))
		return inRequire
	}

	if strings.HasPrefix(trimmed, "require (") || trimmed == "require (" {
		return true
	}

	if inRequire && trimmed == ")" {
		return false
	}

	if strings.HasPrefix(trimmed, "require ") && !inRequire {
		addGoRequire(strings.TrimPrefix(trimmed, "require "), info)
		return inRequire
	}

	if inRequire {
		addGoRequire(trimmed, info)
	}
	return inRequire
}

// addGoRequire parses and appends a require line if valid.
func addGoRequire(line string, info *ManifestInfo) {
	dep := parseGoRequireLine(line)
	if dep.Name != "" {
		info.Dependencies = append(info.Dependencies, dep)
	}
}

// parseGoRequireLine parses a single require line like "github.com/foo/bar v1.2.3 // indirect".
func parseGoRequireLine(line string) Dependency {
	if before, _, found := strings.Cut(line, "//"); found {
		line = before
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return Dependency{}
	}

	parts := strings.Fields(line)
	if len(parts) < 2 {
		return Dependency{}
	}

	return Dependency{
		Name:     parts[0],
		Version:  parts[1],
		Language: "go",
	}
}
