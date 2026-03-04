package polyglot

import (
	"regexp"
	"strings"
)

// CargoInfo contains metadata extracted from a Cargo.toml file.
type CargoInfo struct {
	Name             string
	Version          string
	Edition          string
	Dependencies     []string
	DevDependencies  []string
	WorkspaceMembers []string
}

// ParseCargoToml extracts dependency and metadata from Cargo.toml content.
// Uses simple line-based parsing to avoid adding a TOML dependency.
func ParseCargoToml(data []byte) CargoInfo {
	var info CargoInfo
	lines := strings.Split(string(data), "\n")

	var section string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "[") {
			section = extractSection(trimmed)
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, val := splitKV(trimmed)
		if key == "" {
			continue
		}

		switch section {
		case "package":
			switch key {
			case "name":
				info.Name = val
			case "version":
				info.Version = val
			case "edition":
				info.Edition = val
			}
		case "dependencies":
			info.Dependencies = append(info.Dependencies, key)
		case "dev-dependencies":
			info.DevDependencies = append(info.DevDependencies, key)
		case "workspace":
			if key == "members" {
				info.WorkspaceMembers = parseTomlArray(trimmed)
			}
		}
	}
	return info
}

// extractSection returns the section name from a TOML header like [dependencies].
func extractSection(line string) string {
	line = strings.TrimPrefix(line, "[")
	line = strings.TrimSuffix(line, "]")
	return strings.TrimSpace(line)
}

// splitKV splits "key = value" and strips quotes from value.
func splitKV(line string) (string, string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", ""
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	val = strings.Trim(val, `"'`)
	return key, val
}

var tomlArrayRe = regexp.MustCompile(`"([^"]+)"`)

// parseTomlArray extracts strings from a TOML inline array like ["a", "b", "c"].
func parseTomlArray(line string) []string {
	matches := tomlArrayRe.FindAllStringSubmatch(line, -1)
	result := make([]string, 0, len(matches))
	for _, m := range matches {
		result = append(result, m[1])
	}
	return result
}
