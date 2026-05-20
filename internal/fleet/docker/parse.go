// Package docker provides a fleet.Probe implementation that discovers running
// containers via the local Docker Engine unix socket using raw HTTP/1.1.
// No github.com/docker/docker SDK is used — stdlib net, net/http, bufio,
// encoding/json only.
package docker

import (
	"strings"
)

// imageRef holds the parsed components of a Docker image reference string.
type imageRef struct {
	image  string // registry+repo, no tag, no digest
	tag    string // resolved tag; "" if absent
	digest string // sha256:...; "" if absent
}

// parseImageRef parses a Docker image reference string into components.
//
// Handled forms:
//   - image                      → {image, "latest", ""}
//   - image:tag                  → {image, tag, ""}
//   - image@sha256:digest        → {image, "", "sha256:..."}
//   - image:tag@sha256:digest    → {image, tag, "sha256:..."}
//   - localhost:5000/foo:1.0     → {localhost:5000/foo, 1.0, ""}
//
// Invalid digest format (not sha256:) → digest is silently set to "".
// This follows runtime-probe semantics: partial info is surfaced rather than
// returning an error (contrast with internal/polyglot/pinned which sets Unresolved).
func parseImageRef(ref string) imageRef {
	var r imageRef

	// Split on first "@" for digest.
	if atIdx := strings.Index(ref, "@"); atIdx >= 0 {
		candidate := ref[atIdx+1:]
		ref = ref[:atIdx]
		if strings.HasPrefix(candidate, "sha256:") {
			r.digest = candidate
		}
		// else: invalid digest format — silently drop (r.digest stays "")
	}

	// Parse image:tag using last ":" where suffix has no "/".
	r.image, r.tag = splitImageTag(ref)

	// Apply "latest" default only when no tag AND no digest.
	if r.tag == "" && r.digest == "" {
		r.tag = "latest"
	}

	return r
}

// splitImageTag splits an image reference (without digest) into image and tag.
//
// Rule: find the LAST ":". If the substring after it does NOT contain "/",
// that is the tag. Otherwise the whole string is the image (the colon is part
// of a registry host:port like "localhost:5000/foo").
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
