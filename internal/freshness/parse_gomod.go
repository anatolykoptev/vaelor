package freshness

import (
	"strings"
)

// ParseGoMod extracts dependencies and Go version from go.mod content.
func ParseGoMod(data []byte) ManifestInfo {
	info := ManifestInfo{Language: "go"}
	lines := strings.Split(string(data), "\n")

	inRequire := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}

		// Go version directive.
		if strings.HasPrefix(trimmed, "go ") && !inRequire {
			info.RuntimeVersion = strings.TrimSpace(strings.TrimPrefix(trimmed, "go"))
			continue
		}

		// Start of require block.
		if strings.HasPrefix(trimmed, "require (") || trimmed == "require (" {
			inRequire = true
			continue
		}

		// End of require block.
		if inRequire && trimmed == ")" {
			inRequire = false
			continue
		}

		// Single-line require.
		if strings.HasPrefix(trimmed, "require ") && !inRequire {
			dep := parseGoRequireLine(strings.TrimPrefix(trimmed, "require "))
			if dep.Name != "" {
				info.Dependencies = append(info.Dependencies, dep)
			}
			continue
		}

		// Line inside require block.
		if inRequire {
			dep := parseGoRequireLine(trimmed)
			if dep.Name != "" {
				info.Dependencies = append(info.Dependencies, dep)
			}
		}
	}

	return info
}

// parseGoRequireLine parses a single require line like "github.com/foo/bar v1.2.3 // indirect".
func parseGoRequireLine(line string) Dependency {
	// Strip inline comments.
	if idx := strings.Index(line, "//"); idx >= 0 {
		line = line[:idx]
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
