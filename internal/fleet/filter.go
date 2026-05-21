package fleet

// IsValidFilter reports whether s is acceptable as a filter value passed to
// Filter.Service. Allowed chars: [a-zA-Z0-9._-]. Empty string is allowed
// (means "no filter").
//
// This is the canonical implementation; internal/fleet/docker and
// internal/fleet/ssh both delegate to this. The character set is intentionally
// ASCII-only — Docker container and Compose service names are always ASCII,
// and a tighter allow-list reduces injection surface.
func IsValidFilter(s string) bool {
	for _, r := range s {
		if !isValidFilterRune(r) {
			return false
		}
	}
	return true
}

// isValidFilterRune reports whether r is in [a-zA-Z0-9._-].
// No regexp, no unicode.IsLetter — hand-rolled per security constraint.
func isValidFilterRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '.' || r == '_' || r == '-'
}

// MatchesFilter reports whether a runtime image matches the requested service
// filter, using the documented priority order:
//  1. Exact Container name match
//  2. com.docker.compose.service label value (carried in RuntimeImage.Service)
//
// Empty filter (s == "") always matches.
//
// Both comparisons are case-sensitive.
func MatchesFilter(s string, r RuntimeImage) bool {
	if s == "" {
		return true
	}
	return r.Container == s || r.Service == s
}
