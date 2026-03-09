package freshness

import (
	"strings"
)

const langRuby = "ruby"

// ParseGemfile extracts dependencies from a Ruby Gemfile.
// Uses line-based parsing for gem declarations.
func ParseGemfile(data []byte) ManifestInfo {
	info := ManifestInfo{Language: langRuby}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "source") {
			continue
		}
		if strings.HasPrefix(trimmed, "ruby") {
			info.RuntimeVersion = extractGemQuoted(trimmed[len("ruby"):])
			continue
		}

		if !strings.HasPrefix(trimmed, "gem ") {
			continue
		}

		dep := parseGemLine(trimmed)
		if dep.Name != "" {
			info.Dependencies = append(info.Dependencies, dep)
		}
	}

	return info
}

// parseGemLine parses a gem declaration like: gem 'name', '~> 1.0'
func parseGemLine(line string) Dependency {
	// Remove "gem " prefix.
	line = strings.TrimPrefix(line, "gem ")
	line = strings.TrimSpace(line)

	parts := splitGemArgs(line)
	if len(parts) == 0 {
		return Dependency{}
	}

	name := strings.Trim(parts[0], `"'`)
	if name == "" {
		return Dependency{}
	}

	version := ""
	if len(parts) > 1 {
		version = extractGemVersion(parts[1])
	}

	return Dependency{
		Name:     name,
		Version:  version,
		Language: langRuby,
	}
}

// splitGemArgs splits comma-separated gem arguments respecting quotes.
func splitGemArgs(s string) []string {
	var parts []string
	var current strings.Builder
	inQuote := false
	var quoteChar byte

	for i := range len(s) {
		ch := s[i]
		switch {
		case !inQuote && (ch == '\'' || ch == '"'):
			inQuote = true
			quoteChar = ch
			current.WriteByte(ch)
		case inQuote && ch == quoteChar:
			inQuote = false
			current.WriteByte(ch)
		case !inQuote && ch == ',':
			parts = append(parts, strings.TrimSpace(current.String()))
			current.Reset()
		default:
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, strings.TrimSpace(current.String()))
	}

	return parts
}

// extractGemVersion extracts version from a quoted gem version string.
func extractGemVersion(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return s
}

// extractGemQuoted extracts a quoted string value.
func extractGemQuoted(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	return s
}
