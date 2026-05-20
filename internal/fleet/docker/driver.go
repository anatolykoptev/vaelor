package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
	"unicode"

	"github.com/anatolykoptev/go-code/internal/fleet"
)

// Sentinel errors exposed for callers to errors.Is against.
var (
	// ErrSocketUnavailable is returned when the Driver cannot connect to the
	// Docker Engine unix socket (ENOENT, ECONNREFUSED, etc.).
	ErrSocketUnavailable = errors.New("fleet/docker: socket unavailable")

	// ErrEngineError is returned when the Docker Engine responds with a non-2xx
	// status, returns invalid JSON, or the response body exceeds the size cap.
	ErrEngineError = errors.New("fleet/docker: engine returned error")

	// ErrInvalidFilter is returned when Filter.Service contains characters
	// outside [a-zA-Z0-9._-]. Validated before any network call.
	ErrInvalidFilter = errors.New("fleet/docker: invalid filter")
)

// Driver is the Docker-socket fleet.Probe implementation.
// It communicates with the local Docker Engine via raw HTTP/1.1 over the unix
// socket — no github.com/docker/docker SDK dependency.
//
// Driver implements fleet.Probe. Scheme() returns "docker".
// The alias "local" → docker driver is the caller's concern (P5).
type Driver struct {
	socketPath string
	dial       func(ctx context.Context, network, addr string) (net.Conn, error)
	timeout    time.Duration
}

// Option configures a Driver.
type Option func(*Driver)

// WithSocketPath overrides the default /var/run/docker.sock.
// No os.Getenv is used inside this package; socket path must come via this option.
func WithSocketPath(path string) Option {
	return func(d *Driver) {
		d.socketPath = path
	}
}

// WithDialer overrides the unix-socket dialer. Tests inject net.Pipe-backed
// fakes here to avoid touching the real /var/run/docker.sock.
func WithDialer(fn func(ctx context.Context, network, addr string) (net.Conn, error)) Option {
	return func(d *Driver) {
		d.dial = fn
	}
}

// WithTimeout sets the per-request timeout. Default 10s.
func WithTimeout(duration time.Duration) Option {
	return func(d *Driver) {
		d.timeout = duration
	}
}

// New constructs a Driver with the given options. Callers may use New() with no
// arguments for the default configuration (socket=/var/run/docker.sock, timeout=10s).
func New(opts ...Option) *Driver {
	d := &Driver{
		socketPath: defaultSocketPath,
		timeout:    defaultTimeout,
	}
	for _, o := range opts {
		o(d)
	}
	if d.dial == nil {
		nd := &net.Dialer{}
		d.dial = nd.DialContext
	}
	return d
}

// Scheme returns "docker". The alias "local" → docker driver is wired by the
// Registry caller (P5); this Driver only registers under its canonical scheme.
func (d *Driver) Scheme() string {
	return "docker"
}

// List queries the Docker Engine for running containers and maps them to
// fleet.RuntimeImage values.
//
// Filter.Labels is intentionally ignored for MVP. Per the fleet.Filter spec,
// drivers may omit label matching; P5 wiring handles higher-level filtering.
func (d *Driver) List(ctx context.Context, _ fleet.Target, f fleet.Filter) ([]fleet.RuntimeImage, error) {
	// Validate filter before any I/O.
	if err := validateFilter(f); err != nil {
		return nil, err
	}

	c := newClient(d.socketPath, d.dial, d.timeout)
	containers, err := c.listContainers(ctx)
	if err != nil {
		return nil, err
	}

	imgs := make([]fleet.RuntimeImage, 0, len(containers))
	for _, ctr := range containers {
		img := mapContainer(ctr)
		if f.Service != "" && !matchesFilter(img, f.Service) {
			continue
		}
		imgs = append(imgs, img)
	}
	return imgs, nil
}

// mapContainer converts a raw containerJSON to a fleet.RuntimeImage.
func mapContainer(ctr containerJSON) fleet.RuntimeImage {
	// Resolve container name: first Names entry minus leading slash.
	container := resolveContainerName(ctr.Names, ctr.ID)

	// Parse image reference to extract image, tag, digest.
	ref := parseImageRef(ctr.Image)

	// Resolve StartedAt: Created==0 → zero time; else UTC.
	var startedAt time.Time
	if ctr.Created != 0 {
		startedAt = time.Unix(ctr.Created, 0).UTC()
	}

	// Compose service label.
	service := ctr.Labels["com.docker.compose.service"]

	return fleet.RuntimeImage{
		Container: container,
		Image:     ref.image,
		Tag:       ref.tag,
		Digest:    ref.digest,
		State:     strings.ToLower(ctr.State),
		StartedAt: startedAt,
		Service:   service,
	}
}

// resolveContainerName returns the container's human-readable name.
//
// Priority:
//  1. First entry in Names, with leading slash stripped.
//  2. If Names is empty: first 12 chars of Id (guard: if len(id) < 12, use all of it).
func resolveContainerName(names []string, id string) string {
	if len(names) > 0 {
		return strings.TrimPrefix(names[0], "/")
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// matchesFilter reports whether img satisfies the Service filter.
// Priority:
//  1. Container name exact match
//  2. com.docker.compose.service label exact match
//
// Both comparisons are case-sensitive.
func matchesFilter(img fleet.RuntimeImage, service string) bool {
	return img.Container == service || img.Service == service
}

// validateFilter checks that Filter.Service contains only [a-zA-Z0-9._-] characters.
// Returns a wrapped ErrInvalidFilter if validation fails.
func validateFilter(f fleet.Filter) error {
	if f.Service == "" {
		return nil
	}
	for _, r := range f.Service {
		if !isAllowedFilterChar(r) {
			return fmt.Errorf("%w: service name %q contains invalid character %q",
				ErrInvalidFilter, f.Service, r)
		}
	}
	return nil
}

// isAllowedFilterChar reports whether r is in [a-zA-Z0-9._-].
func isAllowedFilterChar(r rune) bool {
	return unicode.IsLetter(r) && r <= unicode.MaxASCII && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') ||
		r >= '0' && r <= '9' ||
		r == '.' || r == '_' || r == '-'
}
