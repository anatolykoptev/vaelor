package freshness

import (
	"strings"
)

// ParseCargoLock parses a Cargo.lock file and returns a map of
// crate name → resolved version. Only registry packages are included
// (those with a source field pointing to crates.io), so path/git deps
// are excluded naturally because they have no source line.
func ParseCargoLock(data []byte) map[string]string {
	resolved := make(map[string]string)

	var name, version string
	var inPackage bool

	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)

		// Detect start of a new [[package]] block.
		if trimmed == "[[package]]" {
			// Flush previous block if complete.
			if inPackage && name != "" && version != "" {
				resolved[name] = version
			}
			name = ""
			version = ""
			inPackage = true
			continue
		}

		if !inPackage {
			continue
		}

		// Empty line ends a block (Cargo.lock separates blocks with blank lines).
		if trimmed == "" {
			if name != "" && version != "" {
				resolved[name] = version
			}
			name = ""
			version = ""
			inPackage = false
			continue
		}

		key, val := splitCargoLockKV(trimmed)
		switch key {
		case "name":
			name = val
		case "version":
			version = val
		}
	}

	// Flush last block (file may not end with a blank line).
	if inPackage && name != "" && version != "" {
		resolved[name] = version
	}

	return resolved
}

// splitCargoLockKV splits a TOML key = "value" line into key and unquoted value.
// Returns empty strings if the line does not match the expected format.
func splitCargoLockKV(line string) (key, val string) {
	k, v, ok := strings.Cut(line, "=")
	if !ok {
		return "", ""
	}
	key = strings.TrimSpace(k)
	val = strings.TrimSpace(v)
	// Strip surrounding quotes.
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		val = val[1 : len(val)-1]
	}
	return key, val
}

// EnrichWithCargoLock overrides Dependency.Version for Rust deps using
// resolved versions from a parsed Cargo.lock map. Deps not present in
// the lock file are left unchanged.
func EnrichWithCargoLock(deps []Dependency, resolved map[string]string) {
	for i := range deps {
		if deps[i].Language != langRust {
			continue
		}
		if v, ok := resolved[deps[i].Name]; ok {
			deps[i].Version = v
		}
	}
}
