package freshness

import (
	"strings"
)

const langRust = "rust"

// ParseCargoTomlFreshness extracts dependencies with versions from Cargo.toml.
// Similar to polyglot.ParseCargoToml but includes version information.
func ParseCargoTomlFreshness(data []byte) ManifestInfo {
	info := ManifestInfo{Language: langRust}
	var section string
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = extractCargoSection(trimmed)
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		key, val := splitTOMLKV(trimmed)
		if key == "" {
			continue
		}

		switch section {
		case "package":
			parseCargoPackage(key, val, trimmed, &info)
		case "dependencies", "dev-dependencies":
			dep := parseCargoDepLine(key, val, trimmed)
			if dep.Name != "" {
				info.Dependencies = append(info.Dependencies, dep)
			}
		}
	}

	return info
}

// extractCargoSection returns the section name from a TOML header.
func extractCargoSection(line string) string {
	line = strings.TrimPrefix(line, "[")
	line = strings.TrimSuffix(line, "]")
	return strings.TrimSpace(line)
}

// parseCargoPackage handles key-value pairs in the [package] section.
func parseCargoPackage(key, val, _ string, info *ManifestInfo) {
	switch key {
	case "rust-version":
		info.RuntimeVersion = val
	case "edition":
		// Store edition as runtime version if rust-version not set.
		if info.RuntimeVersion == "" {
			info.RuntimeVersion = val
		}
	}
}

// parseCargoDepLine extracts a dependency from a Cargo.toml dependency line.
// Handles both simple `name = "version"` and table `name = { version = "ver" }`.
func parseCargoDepLine(key, val, line string) Dependency {
	version := val

	// Handle inline table: name = { version = "1.0", features = [...] }
	if strings.Contains(val, "{") || strings.Contains(val, "version") {
		version = extractCargoInlineVersion(line)
	}

	return Dependency{
		Name:     key,
		Version:  version,
		Language: langRust,
	}
}

// extractCargoInlineVersion extracts version from inline table syntax.
func extractCargoInlineVersion(line string) string {
	// Look inside the braces for the version key.
	_, inner, found := strings.Cut(line, "{")
	if !found {
		return ""
	}

	const versionKey = "version"
	_, rest, found := strings.Cut(inner, versionKey)
	if !found {
		return ""
	}
	// Skip whitespace and '='.
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, "=")
	rest = strings.TrimSpace(rest)

	// Extract quoted value.
	if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
		quote := rest[0]
		end := strings.IndexByte(rest[1:], quote)
		if end >= 0 {
			return rest[1 : end+1]
		}
	}

	return ""
}
