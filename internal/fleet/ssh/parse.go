package ssh

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/anatolykoptev/vaelor/internal/fleet"
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
	// invalidDigestReason is intentionally ignored here: runtime-probe semantics
	// silently drop invalid digest formats (matches prior behaviour).
	image, tag, digest, _ := fleet.ParseImageRef(rec.Image)
	startedAt := parseCreatedAt(rec.CreatedAt)
	service := ParseComposeService(rec.Labels)

	return fleet.RuntimeImage{
		Container: container,
		Image:     image,
		Tag:       tag,
		Digest:    digest,
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
