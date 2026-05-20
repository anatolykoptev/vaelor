package ssh

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/go-code/internal/fleet"
)

// dockerPSRecord is the JSON structure emitted by:
//
//	docker ps --no-trunc --format={{json .}}
//
// Each line is an independent JSON object (not a JSON array).
//
// Key differences from the Docker HTTP API:
//   - Names is a string (not []string), with no leading slash.
//   - Labels is a comma-separated "key=value" string (not a map).
//   - CreatedAt is a string ("2006-01-02 15:04:05 -0700 MST"), not a Unix int.
//   - No "Created" int field.
type dockerPSRecord struct {
	ID        string `json:"ID"`
	Names     string `json:"Names"`
	Image     string `json:"Image"`
	State     string `json:"State"`
	Labels    string `json:"Labels"`
	CreatedAt string `json:"CreatedAt"`
}

// createdAtLayout is the time format used by `docker ps --format={{json .}}`.
const createdAtLayout = "2006-01-02 15:04:05 -0700 MST"

// ParseDockerPSLine parses a single JSON line from docker ps CLI output and
// returns a fleet.RuntimeImage.
//
// On JSON decode error, an error is returned. On CreatedAt parse error,
// StartedAt is left as zero time.Time{} (no error).
//
// This is exported for use in parse_test.go; the driver calls it internally.
func ParseDockerPSLine(line []byte) (fleet.RuntimeImage, error) {
	var rec dockerPSRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return fleet.RuntimeImage{}, fmt.Errorf("%w: %v", ErrParseError, err)
	}
	return mapRecord(rec), nil
}

// mapRecord converts a dockerPSRecord to a fleet.RuntimeImage.
func mapRecord(rec dockerPSRecord) fleet.RuntimeImage {
	container := resolveContainer(rec.Names, rec.ID)
	ref := parseImageRef(rec.Image)
	startedAt := parseCreatedAt(rec.CreatedAt)
	service := ParseComposeService(rec.Labels)

	return fleet.RuntimeImage{
		Container: container,
		Image:     ref.image,
		Tag:       ref.tag,
		Digest:    ref.digest,
		State:     strings.ToLower(rec.State),
		StartedAt: startedAt,
		Service:   service,
	}
}

// resolveContainer returns the container display name.
// Priority: Names string (already has no leading slash in CLI format).
// Fallback: first 12 characters of ID.
func resolveContainer(names, id string) string {
	if names != "" {
		return names
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// parseCreatedAt parses the CreatedAt string from docker ps CLI output.
// On any error (empty string, garbage) returns zero time.Time{}.
// The caller should not treat a zero StartedAt as an error.
func parseCreatedAt(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(createdAtLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// ParseComposeService extracts the com.docker.compose.service label value from
// a docker ps --format={{json .}} Labels string.
//
// The Labels field is a comma-separated list of "key=value" pairs.
// Returns "" if the key is absent or Labels is empty.
// Exported for parse_test.go.
func ParseComposeService(labels string) string {
	if labels == "" {
		return ""
	}
	for _, pair := range strings.Split(labels, ",") {
		pair = strings.TrimSpace(pair)
		eqIdx := strings.Index(pair, "=")
		if eqIdx < 0 {
			continue
		}
		key := pair[:eqIdx]
		val := pair[eqIdx+1:]
		if key == "com.docker.compose.service" {
			return val
		}
	}
	return ""
}

// imageRef holds the parsed components of a Docker image reference string.
type imageRef struct {
	image  string // registry+repo, no tag, no digest
	tag    string // resolved tag; "" if absent
	digest string // sha256:...; "" if absent
}

// parseImageRef parses a Docker image reference string into components.
// Logic mirrors the docker driver's parseImageRef.
func parseImageRef(ref string) imageRef {
	var r imageRef

	// Split on first "@" for digest.
	if atIdx := strings.Index(ref, "@"); atIdx >= 0 {
		candidate := ref[atIdx+1:]
		ref = ref[:atIdx]
		if strings.HasPrefix(candidate, "sha256:") {
			r.digest = candidate
		}
	}

	r.image, r.tag = splitImageTag(ref)

	// Apply "latest" default only when no tag AND no digest.
	if r.tag == "" && r.digest == "" {
		r.tag = "latest"
	}

	return r
}

// splitImageTag splits an image reference (without digest) into image and tag.
// Rule: find the LAST ":". If the substring after it does NOT contain "/",
// that is the tag. Otherwise the whole string is the image.
func splitImageTag(ref string) (image, tag string) {
	lastColon := strings.LastIndex(ref, ":")
	if lastColon < 0 {
		return ref, ""
	}
	suffix := ref[lastColon+1:]
	if strings.Contains(suffix, "/") {
		return ref, ""
	}
	return ref[:lastColon], suffix
}
