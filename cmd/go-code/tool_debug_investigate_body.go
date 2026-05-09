// cmd/go-code/tool_debug_investigate_body.go
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/anatolykoptev/go-code/internal/analyze"
	"github.com/anatolykoptev/go-code/internal/investigate"
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

// runBodyExtractionPhase populates BodySource on the top-N hypotheses (by
// position, so caller must have already ranked them). Body extraction is
// best-effort: file read errors append a warning to the hypothesis subject
// metadata but never abort the investigation.
//
// topN controls how many hypotheses receive body extraction (task spec: 3).
// Hypotheses with empty File are silently skipped.
func runBodyExtractionPhase(hyps []investigate.Hypothesis, topN int) []investigate.Hypothesis {
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
		endLine := h.EndLine
		if endLine == 0 {
			endLine = h.Line
		}
		body, err := extractBodySource(h.File, h.Line, endLine, 200)
		if err != nil {
			// Best-effort: note the failure but do not propagate error.
			// The warning is visible in the XML diagnostics block.
			_ = err // caller uses BodySource="" as the signal
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
func runBodyExtractionPhaseWithMappings(hyps []investigate.Hypothesis, topN int, mappings []analyze.PathMapping) []investigate.Hypothesis {
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
		// Translate host path → container path for the actual disk read.
		containerPath := rewritePath(h.File, mappings)
		endLine := h.EndLine
		if endLine == 0 {
			endLine = h.Line
		}
		body, err := extractBodySource(containerPath, h.Line, endLine, 200)
		if err != nil {
			// Best-effort: body stays empty, warning is visible in diagnostics.
			continue
		}
		h.BodySource = body
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
		lines := fmt.Sprintf("%d-%d", h.Line, h.EndLine)
		if h.EndLine == 0 || h.EndLine == h.Line {
			lines = fmt.Sprintf("%d", h.Line)
		}
		out = append(out, map[string]any{
			"file":   h.File,
			"lines":  lines,
			"source": h.BodySource,
		})
	}
	return out
}
