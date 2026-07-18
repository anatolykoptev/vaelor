// cmd/go-code/tool_debug_investigate_body.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anatolykoptev/vaelor/internal/analyze"
	"github.com/anatolykoptev/vaelor/internal/investigate"
)

// maxBodyFileBytes is the maximum file size (1 MiB) accepted by extractBodySource.
// Files larger than this are rejected to prevent unbounded memory usage.
const maxBodyFileBytes = 1 * 1024 * 1024

// extractBodySource reads lines [startLine..endLine] (1-based, inclusive) from
// the named file and returns them as a single string. If endLine exceeds the
// actual number of lines, the result is silently trimmed to file length.
// At most maxLines lines are returned; when the excerpt is truncated, a comment
// line "// ... (truncated)" is appended to signal the cut.
//
// Errors:
//   - file not found / unreadable → ("", error)
//   - file larger than maxBodyFileBytes → ("", error)
//   - startLine < 1 → normalised to 1
func extractBodySource(file string, startLine, endLine int, maxLines int) (string, error) {
	info, err := os.Stat(file)
	if err != nil {
		return "", fmt.Errorf("extractBodySource: stat %q: %w", file, err)
	}
	if info.Size() > maxBodyFileBytes {
		return "", fmt.Errorf("extractBodySource: file %q exceeds 1 MiB limit (%d bytes)", file, info.Size())
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return "", fmt.Errorf("extractBodySource: read %q: %w", file, err)
	}

	allLines := strings.Split(string(data), "\n")

	// Normalise bounds (1-based → 0-based indices).
	start := startLine - 1
	if start < 0 {
		start = 0
	}
	end := endLine // exclusive in slice terms; endLine is 1-based inclusive
	if end > len(allLines) {
		end = len(allLines)
	}
	if start >= end {
		return "", nil
	}

	excerpt := allLines[start:end]

	// Cap to maxLines.
	truncated := false
	if len(excerpt) > maxLines {
		excerpt = excerpt[:maxLines]
		truncated = true
	}

	result := strings.Join(excerpt, "\n")
	if truncated {
		result += "\n// ... (truncated)"
	}
	return result, nil
}

// defaultBodyWindow is the line-count heuristic used when EndLine is unknown.
// Rust tracing-opentelemetry emits code.lineno pointing at the function entry
// (the line with #[tracing::instrument(...)] annotation), so reading 1 line
// gives just the annotation. 50 lines covers most function bodies after.
const defaultBodyWindow = 50

// resolveEndLine returns h.EndLine when non-zero. When EndLine is zero (the
// hypothesis came from OTEL code.* tags which only record a single line),
// expand the window by defaultBodyWindow lines so body excerpt covers the
// function body, not just the entry/annotation line.
//
// Tier-3 (callgraph FindSymbol) gives real EndLine from the parser AST and
// hits the first branch.
// Tier-1 (OTEL code.*) hits the second branch and gets the heuristic window.
func resolveEndLine(h *investigate.Hypothesis) int {
	if h.EndLine == 0 {
		return h.Line + defaultBodyWindow
	}
	return h.EndLine
}

// runBodyExtractionPhase populates BodySource on the top-N hypotheses (by
// position, so caller must have already ranked them). Body extraction is
// best-effort: file read errors append a warning to diags.Warnings when diags
// is non-nil. Bodies that exceed maxLines get a "// ... (truncated)" marker.
//
// topN controls how many hypotheses receive body extraction (task spec: 3).
// Hypotheses with empty File are silently skipped.
func runBodyExtractionPhase(hyps []investigate.Hypothesis, topN int, diags *investigate.Diagnostics) []investigate.Hypothesis {
	out := make([]investigate.Hypothesis, len(hyps))
	copy(out, hyps)

	limit := topN
	if limit > len(out) {
		limit = len(out)
	}

	for i := 0; i < limit; i++ {
		h := &out[i]
		if h.File == "" {
			continue
		}
		body, err := extractBodySource(h.File, h.Line, resolveEndLine(h), 200)
		if err != nil {
			if diags != nil {
				diags.Warnings = append(diags.Warnings, fmt.Sprintf("body read %s: %v", h.File, err))
			}
			continue
		}
		h.BodySource = body
	}

	return out
}

// runBodyExtractionPhaseWithMappings is like runBodyExtractionPhase but
// applies PATH_MAPPINGS to translate host-side hypothesis File paths to
// container-internal paths for disk reads. This is required because
// Hypothesis.File is host-side (after reverseToHost), but file reads inside
// the container need the container-internal path (e.g. /host/src/... from the
// /host mount). If mappings is empty, paths are used as-is.
//
// repo is the VCS repo identifier (e.g. "owner/acme-edge"). Its
// last path segment is used as a fallback on-disk directory when the service
// name differs from the repo directory name (e.g. service="web-api-sfu",
// repo dir="acme-edge"). Pass "" when service == repo dir.
//
// File read errors append a warning to diags.Warnings when diags is non-nil,
// giving operators a clear signal on /host mount mis-config or PATH_MAPPINGS
// drift instead of a silent empty body block.
func runBodyExtractionPhaseWithMappings(hyps []investigate.Hypothesis, topN int, service string, repo string, mappings []analyze.PathMapping, diags *investigate.Diagnostics) []investigate.Hypothesis {
	out := make([]investigate.Hypothesis, len(hyps))
	copy(out, hyps)

	limit := topN
	if limit > len(out) {
		limit = len(out)
	}

	for i := 0; i < limit; i++ {
		h := &out[i]
		if h.File == "" {
			continue
		}
		// Try multiple path candidates: absolute, mapped, repo-relative,
		// and /host fallback. Rust tracing-opentelemetry emits relative
		// code.filepath (file!() macro from CARGO_MANIFEST_DIR), which
		// fails as-is; prepending repo or /host gives a working absolute.
		candidates := buildBodyPathCandidates(h.File, service, mappings, repo)
		var body string
		var lastErr error
		for _, p := range candidates {
			b, e := extractBodySource(p, h.Line, resolveEndLine(h), 200)
			if e == nil {
				body = b
				break
			}
			lastErr = e
		}
		if body == "" {
			if diags != nil && lastErr != nil {
				diags.Warnings = append(diags.Warnings, fmt.Sprintf("body read %s (tried %d): %v", h.File, len(candidates), lastErr))
			}
			continue
		}
		h.BodySource = body
	}

	return out
}

// lastPathSegment returns the last non-empty segment of a slash-separated
// path (e.g. "owner/my-repo" → "my-repo", "my-repo" → "my-repo", "" → "").
func lastPathSegment(p string) string {
	p = strings.TrimRight(p, "/")
	if idx := strings.LastIndex(p, "/"); idx >= 0 {
		return p[idx+1:]
	}
	return p
}

// buildBodyPathCandidates returns paths to try in order:
//  1. file as-is (absolute or already mapped)
//  2. rewritePath(file) — host→container mapping
//  3. /host/<file> when file is relative and /host mount exists
//  4. <m.Container>/<file> for each PathMapping when file is relative
//  5. /host/src/<repoDir>/<rel> when repo arg's last segment differs from service
//
// Dedup-protected via filepath.Clean.
// repo is the VCS repo arg (e.g. "owner/acme-edge"); its last path
// segment is used as the on-disk directory when service name ≠ repo dir name.
func buildBodyPathCandidates(file string, service string, mappings []analyze.PathMapping, repo string) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(p string) {
		p = filepath.Clean(p)
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	add(file)
	if mapped := rewritePath(file, mappings); mapped != file {
		add(mapped)
	}
	if !filepath.IsAbs(file) {
		// Try /host/<file> if /host mount is configured anywhere
		for _, m := range mappings {
			if m.External == "/host" {
				add(filepath.Join("/host", file))
				// Service-aware: /host/src/<service>/<rel> covers our
				// convention where each service repo lives under
				// /home/user/src/<service-name>/. Rust file!() emits
				// CARGO_MANIFEST_DIR-relative; we need to anchor it to
				// the actual on-disk repo path inside the /host mount.
				if service != "" {
					add(filepath.Join("/host", "src", service, file))
				}
				// repo-dir fallback: when service name ≠ on-disk repo dir
				// (e.g. service="web-api-sfu", repo dir="acme-edge"),
				// also try the repo dir derived from the repo arg last segment.
				if repoDir := lastPathSegment(repo); repoDir != "" && repoDir != service {
					add(filepath.Join("/host", "src", repoDir, file))
				}
				break
			}
		}
		// Try every mapping's container prefix joined with relative file
		for _, m := range mappings {
			if m.Internal != "" {
				add(filepath.Join(m.Internal, file))
				if service != "" {
					add(filepath.Join(m.Internal, "src", service, file))
				}
				// repo-dir fallback for container mappings
				if repoDir := lastPathSegment(repo); repoDir != "" && repoDir != service {
					add(filepath.Join(m.Internal, "src", repoDir, file))
				}
			}
		}
	}
	// /build/<rel> paths are docker BUILD-time absolute paths (Go CGO builds
	// emit code.filepath=/build/... because that is the container build CWD).
	// When go-code runs on the host, strip the /build prefix and try the
	// host-side repo path via PATH_MAPPINGS or the /host mount.
	if strings.HasPrefix(file, "/build/") && service != "" {
		rel := strings.TrimPrefix(file, "/build/")
		for _, m := range mappings {
			if m.External == "/host" {
				add(filepath.Join("/host", "src", service, rel))
				break
			}
		}
		for _, m := range mappings {
			if m.Internal != "" {
				add(filepath.Join(m.Internal, "src", service, rel))
			}
		}
	}
	return out
}

// collectBodyExcerpts builds the body_excerpts payload for the LLM user
// payload. Only hypotheses with non-empty BodySource are included.
// Returns nil (not empty slice) when nothing has a body — JSON omitempty
// works correctly with nil slice.
func collectBodyExcerpts(hyps []investigate.Hypothesis) []map[string]any {
	var out []map[string]any
	for _, h := range hyps {
		if h.BodySource == "" {
			continue
		}
		var lines string
		if h.EndLine == 0 {
			lines = fmt.Sprintf("%d", h.Line)
		} else {
			lines = fmt.Sprintf("%d-%d", h.Line, h.EndLine)
		}
		out = append(out, map[string]any{
			"file":   h.File,
			"lines":  lines,
			"source": h.BodySource,
		})
	}
	return out
}
