package freshness

import (
	"strings"
)

// ParsePyProject extracts dependencies from pyproject.toml content.
// Uses line-based parsing to avoid adding a TOML library dependency.
func ParsePyProject(data []byte) ManifestInfo {
	info := ManifestInfo{Language: "python"}
	lines := strings.Split(string(data), "\n")

	var section string
	inDepsArray := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.Trim(trimmed, "[]")
			section = strings.TrimSpace(section)
			inDepsArray = false
			continue
		}

		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		switch section {
		case "project":
			parsePyProjectLine(trimmed, &info, &inDepsArray)
		case "project.dependencies":
			// Alternative format: each line is a dependency.
			dep := parsePythonRequirement(trimmed)
			if dep.Name != "" {
				info.Dependencies = append(info.Dependencies, dep)
			}
		}
	}

	return info
}

// parsePyProjectLine handles a line within the [project] section.
func parsePyProjectLine(line string, info *ManifestInfo, inDepsArray *bool) {
	if strings.HasPrefix(line, "requires-python") {
		_, val := splitTOMLKV(line)
		info.RuntimeVersion = val
		return
	}

	if strings.HasPrefix(line, "dependencies") && strings.Contains(line, "[") {
		*inDepsArray = true
		// Check for inline array on same line.
		if idx := strings.Index(line, "["); idx >= 0 {
			inner := line[idx:]
			parsePyDepsArray(inner, info)
			if strings.Contains(inner, "]") {
				*inDepsArray = false
			}
		}
		return
	}

	if *inDepsArray {
		if strings.Contains(line, "]") {
			// Parse any remaining deps before closing bracket.
			before := strings.TrimSuffix(line, "]")
			before = strings.TrimSpace(before)
			if before != "" {
				dep := parsePythonRequirement(strings.Trim(before, `"', `))
				if dep.Name != "" {
					info.Dependencies = append(info.Dependencies, dep)
				}
			}
			*inDepsArray = false
			return
		}
		cleaned := strings.Trim(line, `"', `)
		dep := parsePythonRequirement(cleaned)
		if dep.Name != "" {
			info.Dependencies = append(info.Dependencies, dep)
		}
	}
}

// parsePyDepsArray parses an inline TOML array of dependency strings.
func parsePyDepsArray(s string, info *ManifestInfo) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")

	for _, item := range strings.Split(s, ",") {
		cleaned := strings.Trim(strings.TrimSpace(item), `"'`)
		if cleaned == "" {
			continue
		}
		dep := parsePythonRequirement(cleaned)
		if dep.Name != "" {
			info.Dependencies = append(info.Dependencies, dep)
		}
	}
}

// splitTOMLKV splits a TOML key = "value" line.
func splitTOMLKV(line string) (string, string) {
	parts := strings.SplitN(line, "=", 2)
	if len(parts) != 2 {
		return "", ""
	}
	key := strings.TrimSpace(parts[0])
	val := strings.TrimSpace(parts[1])
	val = strings.Trim(val, `"'`)
	return key, val
}

// ParseRequirementsTxt extracts dependencies from requirements.txt content.
func ParseRequirementsTxt(data []byte) ManifestInfo {
	info := ManifestInfo{Language: "python"}
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "-e") || strings.HasPrefix(trimmed, "-r") {
			continue
		}
		if strings.HasPrefix(trimmed, "--") {
			continue
		}

		dep := parsePythonRequirement(trimmed)
		if dep.Name != "" {
			info.Dependencies = append(info.Dependencies, dep)
		}
	}

	return info
}

// versionOperators lists the operators used in Python version specs,
// ordered longest-first so we match "==" before "=".
var versionOperators = []string{"==", ">=", "<=", "~=", "!=", ">", "<"}

// parsePythonRequirement parses a PEP 508 requirement string like "flask==3.0.0".
func parsePythonRequirement(line string) Dependency {
	// Strip extras like [security].
	if idx := strings.Index(line, "["); idx > 0 {
		rest := ""
		if end := strings.Index(line, "]"); end > idx {
			rest = line[end+1:]
		}
		line = line[:idx] + rest
	}

	// Strip environment markers.
	if idx := strings.Index(line, ";"); idx > 0 {
		line = line[:idx]
	}

	line = strings.TrimSpace(line)

	for _, op := range versionOperators {
		if idx := strings.Index(line, op); idx > 0 {
			name := strings.TrimSpace(line[:idx])
			ver := strings.TrimSpace(line[idx+len(op):])
			// Handle comma-separated version specs: take the first.
			if ci := strings.Index(ver, ","); ci > 0 {
				ver = ver[:ci]
			}
			return Dependency{Name: name, Version: ver, Language: "python"}
		}
	}

	// No version constraint — just a package name.
	if line != "" {
		return Dependency{Name: line, Version: "", Language: "python"}
	}
	return Dependency{}
}
