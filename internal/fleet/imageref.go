package fleet

import (
	"strings"
)

// ParseImageRef splits a Docker image reference into (image, tag, digest,
// invalidDigestReason).
//
// Rules:
//   - If ref contains '@': right side is the digest candidate. If it does NOT
//     begin with "sha256:", digest is set to "" and invalidDigestReason
//     describes why. Otherwise digest = the full "sha256:..." string.
//   - On the remaining ref (or whole if no '@'): find the LAST ':'. If the
//     substring after it does NOT contain '/', that is the tag. Otherwise the
//     whole thing is the image (the colon is part of a registry host:port like
//     "localhost:5000/foo").
//   - If both tag and digest are empty after the above: tag = "latest".
//
// invalidDigestReason is "" when digest is valid or absent.
//
// Driver call sites (fleet/docker, fleet/ssh) ignore invalidDigestReason,
// silently dropping invalid digests — matching the prior silent-drop behaviour.
// The polyglot/pinned parser surfaces it via PinnedImage.Unresolved instead.
func ParseImageRef(ref string) (image, tag, digest, invalidDigestReason string) {
	// Split on first "@" for digest.
	if atIdx := strings.Index(ref, "@"); atIdx >= 0 {
		candidate := ref[atIdx+1:]
		ref = ref[:atIdx]
		if strings.HasPrefix(candidate, "sha256:") {
			digest = candidate
		} else {
			invalidDigestReason = "invalid digest format (expected sha256:): " + candidate
			// digest stays ""
		}
	}

	// Parse image:tag using last ":" where suffix has no "/".
	image, tag = splitImageTagCanonical(ref)

	// Apply "latest" default only when no tag AND no digest.
	if tag == "" && digest == "" {
		tag = "latest"
	}

	return image, tag, digest, invalidDigestReason
}

// splitImageTagCanonical splits an image reference (without digest) into image
// and tag.
//
// Rule: find the LAST ":". If the substring after it does NOT contain "/",
// that is the tag. Otherwise the whole string is the image (the colon is part
// of a registry host:port like "localhost:5000/foo").
func splitImageTagCanonical(ref string) (image, tag string) {
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
