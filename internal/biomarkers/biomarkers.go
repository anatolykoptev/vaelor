// Package biomarkers exposes deterministic file-level signals used to
// estimate near-term defect risk. Each biomarker returns a [0, 1] score
// plus a short human-readable reason. The aggregator combines biomarkers
// with weights to produce a 1-10 file health score.
//
// Inspiration: repowise health-layer methodology (15 biomarkers,
// weights trained on real defect history); we port a narrow subset of
// the two strongest cheap predictors (prior_defect: Kim et al. bug
// cache; churn_risk: Nagappan & Ball ICSE 2005 relative churn).
//
// Zero LLM. All inputs are git history and on-disk file sizes.
package biomarkers

import (
	"context"
	"fmt"
)

// Biomarker computes a single normalised risk signal for a file path
// inside a repo root. Score is in [0, 1] where 1 means "highest risk".
// Reason is a short phrase intended to surface in tool output.
type Biomarker interface {
	Name() string
	Score(ctx context.Context, repoRoot, relPath string) (score float64, reason string, err error)
}

// Registry holds the set of biomarkers available to the aggregator.
// Names must be unique; duplicate registration panics — biomarkers are
// registered once at package init, so this is a programmer error.
type Registry struct {
	order  []string
	byName map[string]Biomarker
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]Biomarker)}
}

// Register adds a biomarker. Panics on name collision.
func (r *Registry) Register(b Biomarker) {
	n := b.Name()
	if _, dup := r.byName[n]; dup {
		panic(fmt.Sprintf("biomarkers: duplicate name %q", n))
	}
	r.byName[n] = b
	r.order = append(r.order, n)
}

// Names returns biomarker names in registration order.
func (r *Registry) Names() []string {
	out := make([]string, len(r.order))
	copy(out, r.order)
	return out
}

// Get returns the biomarker by name, or nil if not registered.
func (r *Registry) Get(name string) Biomarker {
	return r.byName[name]
}

// FileScore is the aggregated result for a single file.
type FileScore struct {
	Path    string             `json:"path"`
	Score   int                `json:"score"`   // 1 = healthy, 10 = on fire
	Reasons map[string]string  `json:"reasons"` // biomarker name → human reason
	Raw     map[string]float64 `json:"raw"`     // biomarker name → [0,1] score
}
