package pinned

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// ParseDockerfile reads a Dockerfile at path and returns one PinnedImage per
// FROM instruction that references an external image. Multi-stage builds are
// handled: non-final stages get a ":builder" suffix on their Service name.
// ARG interpolation is deferred — images starting with "$" are emitted with
// a non-empty Unresolved field.
//
// Intra-Dockerfile stage references (FROM <as-name>) are NOT emitted: if a
// FROM image-ref is a bare identifier (no ':', '@', '/') that exactly matches
// an AS-name declared by a prior FROM in the same file, it is skipped.
//
// Skip predicate details:
//   - Bare identifier: no ':', '@', or '/' in the ref. A ref like
//     "library/base:1.0" will NOT match AS name "base" (has ':' and '/').
//   - If <ref> appears bare but was never declared as an AS-name in this
//     file, it is still emitted (treated as external image with implicit
//     tag=latest). Rare but possible (e.g. scratch, buildpack).
//   - Tracking is per-file — different Dockerfiles never share AS-names.
//
// Final-stage determination uses PHYSICAL FROM count (design choice A):
// skipped FROMs still count toward "last FROM". This preserves the semantic
// that the real base image is a builder when later stages (even skipped ones)
// follow it in the file.
//
// Service naming rules:
//   - Single-stage Dockerfile (regardless of AS clause): Service = ""
//   - Multi-stage final stage with AS <name>: Service = "<name>"
//   - Multi-stage final stage without AS: Service = ""
//   - Multi-stage non-final with AS <name>: Service = "<name>:builder"
//   - Multi-stage non-final without AS: Service = "stage<N>:builder"
func ParseDockerfile(path string) ([]PinnedImage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Collect logical lines from physical lines by collapsing \ continuations.
	// Each entry is (logicalLine, firstPhysicalLineNumber).
	type logLine struct {
		text    string
		lineNum int // 1-based, first physical line
	}
	var logLines []logLine

	scanner := bufio.NewScanner(f)
	var pending strings.Builder
	startLine := 1
	physLine := 0

	for scanner.Scan() {
		physLine++
		raw := scanner.Text()

		if pending.Len() == 0 {
			startLine = physLine
		}

		if strings.HasSuffix(raw, "\\") {
			// Strip trailing backslash and accumulate.
			pending.WriteString(strings.TrimRight(raw, "\\"))
			pending.WriteString(" ")
			continue
		}

		// End of logical line.
		pending.WriteString(raw)
		logLines = append(logLines, logLine{
			text:    strings.TrimSpace(pending.String()),
			lineNum: startLine,
		})
		pending.Reset()
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	// Handle trailing continuation (malformed but be tolerant).
	if pending.Len() > 0 {
		logLines = append(logLines, logLine{
			text:    strings.TrimSpace(pending.String()),
			lineNum: startLine,
		})
	}

	// First pass: collect all FROM statements.
	type fromEntry struct {
		imageRef  string
		stageName string // from AS clause, or "" if absent
		lineNum   int
		idx       int // 0-based index among all FROMs in this file
	}
	var froms []fromEntry

	for _, ll := range logLines {
		line := ll.text
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "FROM") {
			continue
		}
		rest := strings.TrimSpace(line[4:])
		if rest == "" {
			continue
		}

		// Parse: <imageRef> [AS <stage>]
		imageRef, stageName := parseFromClause(rest)
		froms = append(froms, fromEntry{
			imageRef:  imageRef,
			stageName: stageName,
			lineNum:   ll.lineNum,
			idx:       len(froms),
		})
	}

	if len(froms) == 0 {
		return nil, nil
	}

	isOnly := len(froms) == 1

	// Build a set of AS-names declared so far in this file.
	// Used to detect intra-Dockerfile stage references.
	// Populated incrementally: a name is only available to FROMs that follow it.
	asNames := make(map[string]struct{}, len(froms))

	// Second pass: build PinnedImage for each FROM.
	var result []PinnedImage
	for i, fe := range froms {
		// isFinal uses PHYSICAL FROM count (design choice A): skipped FROMs
		// still count, so the first real base image is correctly non-final
		// in multi-stage layouts where all subsequent FROMs are stage-refs.
		isFinal := i == len(froms)-1

		// Check if this FROM is an intra-Dockerfile stage reference.
		// Skip condition: image-ref is a bare identifier (no ':', '@', '/')
		// AND matches a previously-declared AS-name in this file.
		if isStageRef(fe.imageRef, asNames) {
			// Record AS-name declared by this line (if any) before skipping,
			// so later FROMs can also reference it.
			if fe.stageName != "" {
				asNames[fe.stageName] = struct{}{}
			}
			continue
		}

		// Record this FROM's AS-name AFTER the skip check (a name is only
		// reachable by FROMs that come after it in the file).
		if fe.stageName != "" {
			asNames[fe.stageName] = struct{}{}
		}

		// Determine service field.
		var service string
		switch {
		case isOnly:
			// Single-stage Dockerfiles never use the stage name as Service.
			service = ""
		case isFinal:
			// Final stage: use AS name if present, else "".
			service = fe.stageName
		default:
			// Non-final stage.
			svcName := fe.stageName
			if svcName == "" {
				svcName = "stage" + strconv.Itoa(fe.idx)
			}
			service = svcName + ":builder"
		}

		// Parse the image reference.
		pi := parseImageRef(fe.imageRef)
		pi.Service = service
		pi.Line = fe.lineNum
		pi.Source = path
		result = append(result, pi)
	}

	return result, nil
}

// isStageRef returns true when ref is a bare identifier (contains none of
// ':', '@', '/') AND matches a previously-declared AS-name in this Dockerfile.
//
// The bare-identifier check ensures that refs like "library/base:1.0" or
// "registry.example.com:5000/base" are never mistakenly skipped even if a
// prior stage was declared AS "base".
func isStageRef(ref string, asNames map[string]struct{}) bool {
	if strings.ContainsAny(ref, ":@/") {
		return false
	}
	_, ok := asNames[ref]
	return ok
}

// parseFromClause splits the rest of a FROM line into imageRef and stage name.
// Input example: "golang:1.26-alpine AS builder" or "redis@sha256:..." or "$BASE AS builder"
func parseFromClause(s string) (imageRef, stageName string) {
	// Look for " AS " (case-insensitive) to split off stage name.
	upper := strings.ToUpper(s)
	asIdx := strings.Index(upper, " AS ")
	if asIdx >= 0 {
		imageRef = strings.TrimSpace(s[:asIdx])
		stageName = strings.TrimSpace(s[asIdx+4:])
		return
	}
	imageRef = strings.TrimSpace(s)
	return
}

// parseImageRef parses an image reference string into a PinnedImage.
// It handles:
//   - image@digest
//   - image:tag@digest
//   - image:tag
//   - image (implies tag=latest)
//   - $VAR or ${VAR} — emits Unresolved
func parseImageRef(ref string) PinnedImage {
	// ARG interpolation check.
	if strings.HasPrefix(ref, "$") {
		return PinnedImage{
			Image:      ref,
			Unresolved: "ARG interpolation not supported: FROM " + ref,
		}
	}

	var pi PinnedImage

	// Split on first "@" for digest.
	atIdx := strings.Index(ref, "@")
	if atIdx >= 0 {
		pi.Digest = ref[atIdx+1:]
		ref = ref[:atIdx]
		if !strings.HasPrefix(pi.Digest, "sha256:") {
			pi.Unresolved = "invalid digest format: " + pi.Digest
		}
	}

	// Parse image:tag using last ":" where suffix has no "/".
	pi.Image, pi.Tag = splitImageTag(ref)

	// Apply "latest" default only when no tag AND no digest.
	if pi.Tag == "" && pi.Digest == "" {
		pi.Tag = "latest"
	}

	return pi
}

// splitImageTag splits an image reference into image and tag.
// Rule: find the LAST ":". If the substring after it does NOT contain "/",
// that is the tag. Otherwise the whole string is the image (no tag).
func splitImageTag(ref string) (image, tag string) {
	lastColon := strings.LastIndex(ref, ":")
	if lastColon < 0 {
		return ref, ""
	}
	suffix := ref[lastColon+1:]
	if strings.Contains(suffix, "/") {
		// Colon is part of a registry host:port, not a tag separator.
		return ref, ""
	}
	return ref[:lastColon], suffix
}
