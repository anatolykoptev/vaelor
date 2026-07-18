// Package fleet provides the runtime-image discovery primitives for go-code.
//
// It defines the core data carriers (RuntimeImage, Target, Filter), the Probe
// interface, a Registry for mapping URL schemes to Probe drivers, and a pure
// Diff function for comparing pinned source images against discovered runtime
// images.
//
// Package fleet imports internal/polyglot/pinned for the PinnedImage type.
// All driver packages (fleet/docker, fleet/ssh, …) depend on fleet, not the
// other way around.
package fleet

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/anatolykoptev/vaelor/internal/polyglot/pinned"
)

// Ensure pinned is used (imported for ImageDiff.Pinned field type).
var _ = (*pinned.PinnedImage)(nil)

// RuntimeImage is one container image discovered by a Probe on a target host.
type RuntimeImage struct {
	Container string    // container name (preferred) or short id
	Image     string    // registry+repo, no tag, no digest
	Tag       string    // resolved tag; "" if image lacks one
	Digest    string    // sha256:...; "" if not surfaced by probe
	State     string    // "running" / "exited" / "restarting" / etc — probe-driver canonical
	StartedAt time.Time // zero if unknown
	Service   string    // com.docker.compose.service label value if present, else ""
}

// Target identifies WHERE a probe should look. Scheme dispatch happens in Registry.Get.
type Target struct {
	Raw    string // original input, kept for diagnostics
	Scheme string // "local" | "docker" | "ssh" (case-folded to lowercase)
	Host   string // ssh: hostname or alias as understood by ~/.ssh/config. docker/local: ""
	User   string // ssh-only; "" means default per ~/.ssh/config
	Port   int    // ssh-only; 0 means default per ~/.ssh/config
}

// Filter narrows what a probe returns. Empty fields mean no constraint.
type Filter struct {
	Service string            // matches container name OR com.docker.compose.service label OR plain compose service
	Labels  map[string]string // exact label-value match; reserved for v2 — drivers may ignore for MVP
}

// ErrSchemeUnknown is returned by Registry.Get when no probe is registered.
var ErrSchemeUnknown = errors.New("fleet: no probe registered for scheme")

// Probe is the runtime-image discovery primitive. One implementation per Scheme.
type Probe interface {
	Scheme() string
	List(ctx context.Context, t Target, f Filter) ([]RuntimeImage, error)
}

// Registry maps a target scheme to its Probe driver.
type Registry struct {
	drivers map[string]Probe
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{drivers: make(map[string]Probe)}
}

// Register adds p under p.Scheme(). Overwrites silently if scheme already present
// (last-wins) — caller can detect via Has().
func (r *Registry) Register(p Probe) {
	r.drivers[p.Scheme()] = p
}

// Has reports whether a probe is registered for the given scheme.
func (r *Registry) Has(scheme string) bool {
	_, ok := r.drivers[scheme]
	return ok
}

// Get returns the Probe for t.Scheme. Error if no probe is registered.
// Error wraps a sentinel ErrSchemeUnknown that callers can errors.Is against.
func (r *Registry) Get(t Target) (Probe, error) {
	p, ok := r.drivers[t.Scheme]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrSchemeUnknown, t.Scheme)
	}
	return p, nil
}
